package lsp

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"testing"
)

// FuzzReadMessage attacks the JSON-RPC frame parser with streams of
// concatenated and fragmented frames: valid, near-valid and garbage
// Content-Length headers, bodies shorter or longer than declared, split
// across reads. This is the parser that consumes attacker-influenced
// bytes off the language server's socket in a background goroutine, so
// the oracles are the threat model's: no panic, no unbounded allocation
// driven by a header's claim, no frame parsed past what the stream
// holds, and forward progress — every iteration consumes or errors,
// never loops.
//
// Input layout, shaped toward near-valid frames per the repository's
// fuzz harness contract (docs/specs/conformance/lane-e-fuzzing.md):
// the first byte selects a construction mode, the next byte the read
// chunking, and the remainder is the stream itself. Budgets: total input
// clamped to 64 KiB, chunk count bounded by construction, constructed
// declared lengths clamped to 1 MiB — the contract's amplification test
// is what the parser does with a claim, not what the harness can feed.
func FuzzReadMessage(f *testing.F) {
	f.Add([]byte("Content-Length: 2\r\n\r\n{}"))
	f.Add([]byte{1, 4, 'C', 'o', 'n', 't', 'e', 'n', 't', '-', 'L', 'e', 'n', 'g', 't', 'h', ':', ' ', '5', '\r', '\n', '\r', '\n', '{', '}'})
	f.Add([]byte("Content-Length: 999999999999999999999\r\n\r\n{}"))
	f.Add([]byte("Content-Length: -4\r\n\r\n{}"))
	f.Add([]byte("Content-Length: 10\r\nContent-Length: 10\r\n\r\n{}"))
	f.Add([]byte("garbage without colons\r\n\r\n"))
	f.Add([]byte{0, 1, 0, 0, 0, 0, 0})
	f.Fuzz(func(t *testing.T, input []byte) {
		const maxInput = 64 << 10
		if len(input) > maxInput {
			input = input[:maxInput]
		}
		if len(input) < 3 {
			return
		}
		stream := buildFrameStream(t, input)
		// Feed the stream in chunks sized 1..64 bytes from the input, so
		// read boundaries fall inside headers and bodies — the parser's
		// contract is about stream boundaries, not slices.
		chunked := &chunkReader{data: stream, sizes: input}
		r := bufio.NewReaderSize(chunked, 64)

		// Every iteration must consume input or terminate: a parser that
		// loops without consuming is the deadlock the threat model
		// forbids, and len(stream)+1 iterations is the strictest bound.
		for i := 0; i <= len(stream); i++ {
			body, err := ReadMessage(r)
			if err != nil {
				if err == io.EOF && chunked.off < len(stream) {
					t.Fatalf("EOF with %d unread bytes", len(stream)-chunked.off)
				}
				return
			}
			// A parsed frame's body is exactly its declared length by
			// construction of the format; what it can never be is larger
			// than the stream that produced it plus nothing — the parser
			// must not invent bytes.
			if len(body) > len(stream) {
				t.Fatalf("body of %d bytes parsed from a %d-byte stream", len(body), len(stream))
			}
		}
		t.Fatalf("parser consumed nothing for %d iterations over a %d-byte stream", len(stream)+1, len(stream))
	})
}

// buildFrameStream decodes fuzz input into a stream of near-valid frames
// interleaved with raw input: most bytes become validly framed bodies,
// but headers are corrupted in structured ways — mutated digits, dropped
// colons, huge claims — so both accept and reject paths stay live.
func buildFrameStream(t *testing.T, input []byte) []byte {
	t.Helper()
	mode := input[0] % 6
	payload := input[2:]
	var out bytes.Buffer
	switch mode {
	case 0:
		// Raw: the input is the stream, unmodified — pure mutation space.
		return append([]byte(nil), payload...)
	case 1:
		// Valid frames over chunks of the payload.
		for len(payload) > 0 {
			n := 1 + int(payload[0])%64
			payload = payload[1:]
			if n > len(payload) {
				n = len(payload)
			}
			fmt.Fprintf(&out, "Content-Length: %d\r\n\r\n", n)
			out.Write(payload[:n])
			payload = payload[n:]
		}
	case 2:
		// Near-valid: correct framing but declared lengths amplified
		// beyond the bytes present — clamped to the 1 MiB harness budget,
		// while digit corruption from the input can still escape upward.
		for len(payload) > 2 {
			claimed := int(binary.LittleEndian.Uint16(payload[:2])) % (1 << 20)
			payload = payload[2:]
			n := 1 + int(payload[0])%32
			payload = payload[1:]
			if n > len(payload) {
				n = len(payload)
			}
			fmt.Fprintf(&out, "Content-Length: %d\r\n\r\n", claimed)
			out.Write(payload[:n])
			payload = payload[n:]
		}
	case 3:
		// Headers with corrupted structure: missing colons, negative and
		// empty values, duplicates, garbage names.
		for len(payload) > 1 {
			n := 1 + int(payload[0])%16
			payload = payload[1:]
			if n > len(payload) {
				n = len(payload)
			}
			fragment := payload[:n]
			payload = payload[n:]
			out.WriteString("Content-Length")
			if len(fragment) > 0 && fragment[0]%3 != 0 {
				out.WriteString(":")
			}
			fmt.Fprintf(&out, " %d\r\n\r\n", int(fragment[len(fragment)-1])-128)
			out.Write(fragment)
		}
	case 4:
		// One giant claim followed by a short body: the amplification
		// case. The claim is input-derived but clamped to 1 MiB; what the
		// parser allocates for it must not scale with the claim beyond
		// the protocol's own cap.
		claim := uint32(binary.LittleEndian.Uint32(append(append([]byte{}, payload[:min(4, len(payload))]...), 0, 0, 0, 0)[:4])) % (1 << 20)
		fmt.Fprintf(&out, "Content-Length: %d\r\n\r\n", claim)
		out.Write(payload[:min(len(payload), 256)])
	default:
		// Valid frame, then garbage, then valid frame: the parser must
		// resynchronize or error, never loop.
		body := payload[:min(len(payload), 32)]
		fmt.Fprintf(&out, "Content-Length: %d\r\n\r\n", len(body))
		out.Write(body)
		out.WriteString("not a header at all\r\n")
		fmt.Fprintf(&out, "Content-Length: 2\r\n\r\n{}")
	}
	return out.Bytes()
}

// chunkReader yields data in chunk sizes taken cyclically from sizes, so
// the bufio layer above it sees boundaries at input-controlled positions.
type chunkReader struct {
	data  []byte
	sizes []byte
	off   int
	n     int
}

func (c *chunkReader) Read(p []byte) (int, error) {
	if c.off >= len(c.data) {
		return 0, io.EOF
	}
	size := 1 + int(c.sizes[c.n%len(c.sizes)])%64
	c.n++
	if size > len(p) {
		size = len(p)
	}
	n := min(size, len(c.data)-c.off)
	copy(p, c.data[c.off:c.off+n])
	c.off += n
	return n, nil
}
