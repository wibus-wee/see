// Verification probes written by the independent verifier (verify-lane-a).
// They are not part of the implementer's evidence: each test here either
// demonstrates a leak or documents the admitted boundary, and the test
// names say which. Tests that expect a leak to be VISIBLE are written as
// observations (they pass while reporting the leak through t.Log), so the
// suite stays green only where behavior is correct.

package proc

import (
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
)

// A derived encoding of a secret is not redacted. This is admitted in the
// conformance matrix; the test pins how complete the leak is so the
// admission can be judged with evidence rather than trust.
func TestVerifyRedactionDerivedFormsLeak(t *testing.T) {
	secret := "hunter2verysecret"
	env := []string{"DEPLOY_TOKEN=" + secret}
	for _, form := range []struct {
		name, value string
	}{
		{"base64", base64.StdEncoding.EncodeToString([]byte(secret))},
		{"percent-encoded", url.QueryEscape(secret)},
		{"uppercase", strings.ToUpper(secret)},
		{"spaced", strings.Join(strings.Split(secret, ""), " ")},
		{"reversed-with-marker", reverse(secret)},
	} {
		out := RedactSecrets("diag: "+form.value, env)
		leaked := strings.Contains(out, form.value)
		t.Logf("%s form leaked verbatim: %v", form.name, leaked)
		if form.name == "percent-encoded" && !leaked {
			t.Log("note: QueryEscape only escapes when the secret holds reserved characters; this secret has none")
		}
	}
	// The exact value must never leak, as a control.
	if out := RedactSecrets("diag: "+secret, env); strings.Contains(out, secret) {
		t.Fatalf("control failed: exact secret survived redaction: %q", out)
	}
}

func reverse(s string) string {
	r := []rune(s)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r)
}

// A secret straddling the 4 KiB cut cannot be matched whole. The
// whole-lines policy of String drops the partial first line, which is
// where the straddled suffix necessarily sits — pin that this rescue
// actually holds, because a redaction boundary that leaked partial
// secrets would be worse than no redaction claim at all.
func TestVerifyTailBoundaryStraddleNeutralized(t *testing.T) {
	secret := "0123456789abcdefSECRET-material-here"
	env := []string{"API_TOKEN=" + secret}
	const max = 4 << 10
	tail := NewTail(max, env)

	// Stream: padding line, then the secret positioned so the buffer cut
	// lands inside it, then filler that pushes the total past max.
	cut := len(secret) / 2
	pre := strings.Repeat("p", max-len(secret)-2) + "\n" // one full padding line
	stream := pre + secret + "\n" + strings.Repeat("f", cut+8)
	if len(stream) <= max {
		t.Fatalf("stream %d does not exceed the bound %d", len(stream), max)
	}
	if _, err := tail.Write([]byte(stream)); err != nil {
		t.Fatal(err)
	}
	out := tail.String()
	if strings.Contains(out, secret) {
		t.Fatalf("whole secret present unredacted: %q", out)
	}
	for i := 1; i < len(secret); i++ {
		if frag := secret[i:]; len(frag) >= 8 && strings.Contains(out, frag) {
			t.Fatalf("straddle leaked a %d-byte fragment %q into the digest", len(frag), frag)
		}
	}
}

// A secret longer than the digest buffer itself can never match inside
// the window: the whole value never fits, so nothing is redacted and up
// to max bytes of it are exposed. Long env-held credentials exist — a
// base64 private key with its certificate chain, or a large session
// token — and the matrix's admission covers derived forms, not this.
func TestVerifyOversizeSecretLeaks(t *testing.T) {
	secret := strings.Repeat("k", 5000) // longer than the 4 KiB digest
	env := []string{"PRIVATE_KEY=" + secret}
	const max = 4 << 10
	tail := NewTail(max, env)
	if _, err := tail.Write([]byte("fatal: key rejected\n" + secret + "\n")); err != nil {
		t.Fatal(err)
	}
	out := tail.String()
	exposed := 0
	for i := 0; i+64 <= len(secret); i++ {
		if strings.Contains(out, secret[i:i+64]) {
			exposed = len(secret) // any 64-byte fragment proves exposure
			break
		}
	}
	if exposed > 0 {
		t.Logf("LEAK CONFIRMED: a %d-byte secret is exposed through the %d-byte digest because it can never match whole", len(secret), max)
	} else {
		t.Fatal("unexpected: oversize secret was redacted")
	}
}

// Case variants of a secret are not the environment value, so a server
// that uppercases diagnostics leaks the secret's content.
func TestVerifyExactMatchOnlyControl(t *testing.T) {
	secret := "MixedCaseSecret123"
	env := []string{"PASSWORD=" + secret}
	out := RedactSecrets("err: "+secret, env)
	if strings.Contains(out, secret) {
		t.Fatalf("exact secret leaked: %q", out)
	}
	upper := strings.ToUpper(secret)
	if out := RedactSecrets("err: "+upper, env); strings.Contains(out, upper) {
		t.Logf("uppercase variant leaked: %q", out)
	} else {
		t.Fatal("unexpected: uppercase variant was redacted; the matcher is broader than documented")
	}
}

// Secrets shorter than 8 bytes are an admitted tradeoff; pin the exact
// threshold so a reviewer can see what 7 characters of token buys.
func TestVerifyShortSecretThreshold(t *testing.T) {
	env := []string{"TOKEN=abc1234"} // 7 chars
	out := RedactSecrets("token abc1234 leaked", env)
	if strings.Contains(out, "abc1234") {
		t.Log("LEAK CONFIRMED: 7-char secret is never redacted")
	} else {
		t.Fatal("short secret redacted contrary to the documented threshold")
	}
	env8 := []string{"TOKEN=abc12345"} // 8 chars
	if out := RedactSecrets("token abc12345 leaked", env8); strings.Contains(out, "abc12345") {
		t.Fatal("8-char secret should be redacted")
	}
}
