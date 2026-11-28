package proc

// Independent re-verification probes for the streaming stderr redaction
// (fix of verify-lane-a F1). These tests are adversarial constructions of
// the re-verifier, not copies of the implementer's tests: positions,
// chunkings and secret shapes are chosen to attack the streaming
// implementation's typical weak points — emission before a split secret
// completes, the trim order relative to redaction, holdback sizing when
// secrets of mixed lengths coexist, and URL-password secrets.

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

// fragmentsOf returns every window of size n of secret, stepped coarsely,
// so a test can assert no sizeable fragment survives anywhere.
func fragmentsOf(secret string, n int) []string {
	var out []string
	for i := 0; i+n <= len(secret); i += n {
		out = append(out, secret[i:i+n])
	}
	return out
}

func assertNoFragments(t *testing.T, out, secret string, n int) {
	t.Helper()
	for _, frag := range fragmentsOf(secret, n) {
		if strings.Contains(out, frag) {
			t.Fatalf("secret fragment %.40q… leaked into digest", frag)
		}
	}
}

// writeIn writes data to tail in chunks of size n.
func writeIn(tail *Tail, data string, n int) {
	for len(data) > 0 {
		k := min(len(data), n)
		tail.Write([]byte(data[:k]))
		data = data[k:]
	}
}

// An oversize secret is attacked at every position in the stream: flush at
// the start, at the end of output, in the middle of a flood, and straddling
// the 4 KiB window cut with its tail landing exactly at end of stream — the
// construction where trim-before-redact leaks the whole tail half.
func TestReverifyOversizeSecretAtEveryPosition(t *testing.T) {
	secret := strings.Repeat("PWNED-", 834) // 5004 bytes, distinctive
	env := []string{"TLS_PRIVATE_KEY=" + secret}
	const max = 4 << 10
	for _, tc := range []struct {
		name   string
		stream string
	}{
		{"at start", secret + "\nfatal: crashed\n"},
		{"at end no newline", "boom\n" + secret},
		{"mid flood", strings.Repeat("x", 2000) + "\n" + secret + "\n" + strings.Repeat("y", 2000)},
		{"cut lands mid-secret, secret ends at end of stream", strings.Repeat("q", max-len(secret)/2) + secret},
		{"straddling the cut", strings.Repeat("q", max-len(secret)/2-10) + "\n" + secret + "\nzz"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if len(tc.stream) <= max && tc.name != "at end no newline" && tc.name != "at start" {
				t.Fatalf("stream %d does not exceed the window %d: the straddle is untested", len(tc.stream), max)
			}
			tail := NewTail(max, env)
			tail.Write([]byte(tc.stream))
			out := tail.String()
			assertNoFragments(t, out, secret, 64)
			if strings.Count(out, "<redacted>") == 0 {
				t.Fatalf("no redaction marker in %.120q", out)
			}
		})
	}
}

// The canonical streaming-redaction exploit: emit fragments of a secret in
// chunks smaller than the secret, arranged so naive implementations flush
// partial bytes into the window before the secret completes. Three write
// schedules attack the holdback: fixed small chunks, prime-sized chunks
// (aliasing-resistant), and alternating tiny/large chunks.
func TestReverifySecretSplitInAdversarialSchedules(t *testing.T) {
	secret := "sk-live-9f8e7d6c5b4a3982716fedcbanotherchunk"
	env := []string{"STRIPE_API_KEY=" + secret}
	data := "auth failed for key " + secret + " — retrying\n"
	for _, tc := range []struct {
		name  string
		write func(tail *Tail)
	}{
		{"8-byte chunks", func(tail *Tail) { writeIn(tail, data, 8) }},
		{"prime 97-byte chunks", func(tail *Tail) { writeIn(tail, data, 97) }},
		{"alternating 1 and 300", func(tail *Tail) {
			rest := data
			n := 1
			for len(rest) > 0 {
				k := min(len(rest), n)
				tail.Write([]byte(rest[:k]))
				rest = rest[k:]
				if n == 1 {
					n = 300
				} else {
					n = 1
				}
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tail := NewTail(4<<10, env)
			tc.write(tail)
			out := tail.String()
			assertNoFragments(t, out, secret, 12)
			if !strings.Contains(out, "<redacted>") {
				t.Fatalf("redaction marker missing: %q", out)
			}
			if !strings.Contains(out, "auth failed for key") {
				t.Fatalf("harmless context lost: %q", out)
			}
		})
	}

	// Two writes, the split point swept across every byte inside the
	// secret: no position of the pipe-read boundary may leak a fragment.
	t.Run("split inside the secret at every byte", func(t *testing.T) {
		for split := 1; split < len(secret); split++ {
			inner := NewTail(4<<10, env)
			at := len("auth failed for key ") + split
			inner.Write([]byte(data[:at]))
			inner.Write([]byte(data[at:]))
			out := inner.String()
			assertNoFragments(t, out, secret, 12)
			if !strings.Contains(out, "<redacted>") {
				t.Fatalf("split at byte %d: redaction marker missing: %q", split, out)
			}
		}
	})
}

// Holdback sizing with mixed secret lengths: a 5 KiB secret in the
// environment inflates the holdback far beyond a second, short secret. The
// short secret must still be redacted when it completes — and the digest
// must still emit output promptly rather than starving behind the giant
// holdback when no secret is present.
func TestReverifyMixedLengthSecretsHoldback(t *testing.T) {
	big := strings.Repeat("B", 5000)
	small := "short-token-abcdef123456"
	env := []string{"HUGE_PEM=" + big, "API_SECRET=" + small}
	tail := NewTail(4<<10, env)
	// Interleave: noise, split small secret across writes, more noise.
	tail.Write([]byte("noise line\npart1: " + small[:10]))
	tail.Write([]byte(small[10:] + " end\n"))
	tail.Write([]byte(strings.Repeat("n", 6000)))
	out := tail.String()
	assertNoFragments(t, out, small, 8)
	// The digest must still carry output: noise must be present.
	if !strings.Contains(out, strings.Repeat("n", 100)) {
		t.Fatalf("output starved behind holdback: %.80q", out)
	}
	// And the big secret redacts when it does arrive, byte by byte.
	tail2 := NewTail(4<<10, env)
	writeIn(tail2, big, 1)
	assertNoFragments(t, tail2.String(), big, 64)
}

// URL passwords are redacted as their own secret even when the process
// prints only the password, split across writes — a server echoing
// credentials it parsed out of a URL.
func TestReverifyURLPasswordSplitAcrossWrites(t *testing.T) {
	password := "px9wQ7vK2mN4bT8yL3"
	env := []string{"GOPROXY=https://deploy:" + password + "@proxy.corp.example"}
	tail := NewTail(4<<10, env)
	writeIn(tail, "dial "+password+" failed\n", 3)
	out := tail.String()
	if strings.Contains(out, password) {
		t.Fatalf("url password leaked: %q", out)
	}
}

// Documentary probe for a residual shape the design cannot close: output
// that ends with a strict prefix of a secret is emitted verbatim at the
// final flush, because it never completes. This pins how much of a secret
// can leak when a process dies mid-print — up to len(secret)-1 bytes.
// Exact-match redaction cannot do better; the matrix admits exact-match
// semantics, but the size of the residual is worth knowing.
func TestReverifyPartialSecretAtEndOfStreamPinsResidual(t *testing.T) {
	secret := "token-0123456789abcdef0123456789abcdef"
	env := []string{"TOKEN=" + secret}
	tail := NewTail(4<<10, env)
	tail.Write([]byte("dying: " + secret[:len(secret)-1])) // one byte short
	out := tail.String()
	if strings.Contains(out, secret) {
		t.Fatal("impossible: the secret was never printed in full")
	}
	leaked := strings.Contains(out, secret[:len(secret)-1])
	t.Logf("residual shape: strict prefix of len %d leaked verbatim at end of stream: %v", len(secret)-1, leaked)
}

// Derived-form leaks remain by design: the matrix admits them as the only
// leak class. This probe confirms the admission is real (not overstated
// redaction) — a base64-encoded secret and a case-flipped secret both pass
// through verbatim.
func TestReverifyDerivedFormsStillLeakAsAdmitted(t *testing.T) {
	secret := "correct-horse-battery-staple-9f8e7d"
	env := []string{"PASSWORD=" + secret}
	encoded := base64.StdEncoding.EncodeToString([]byte(secret))
	flipped := strings.ToUpper(secret)

	tail := NewTail(4<<10, env)
	tail.Write([]byte("b64: " + encoded + "\n"))
	if !strings.Contains(tail.String(), encoded) {
		t.Fatal("expectation broken: base64 derived form should still pass through (admitted design limit)")
	}

	tail2 := NewTail(4<<10, env)
	tail2.Write([]byte("upper: " + flipped + "\n"))
	if !strings.Contains(tail2.String(), flipped) {
		t.Fatal("expectation broken: case-flipped derived form should still pass through (admitted design limit)")
	}

	// Control: the exact form never passes.
	tail3 := NewTail(4<<10, env)
	tail3.Write([]byte("exact: " + secret + "\n"))
	if strings.Contains(tail3.String(), secret) {
		t.Fatal("exact secret leaked — the base redaction is broken")
	}
}

// Regression pin for the holdback accounting: after heavy traffic with no
// secrets, memory inside the tail stays bounded — pending never grows past
// the holdback, verified indirectly by output correctness under a flood
// that ends with an intact secret.
func TestReverifySecretAfterLongFlood(t *testing.T) {
	secret := "flood-end-secret-0123456789"
	env := []string{"SESSION_TOKEN=" + secret}
	tail := NewTail(4<<10, env)
	for i := 0; i < 200; i++ {
		tail.Write([]byte(fmt.Sprintf("line %d of noise\n", i)))
	}
	writeIn(tail, "tail-end "+secret, 5)
	out := tail.String()
	if strings.Contains(out, secret) {
		t.Fatalf("secret after flood leaked: %.100q", out)
	}
}
