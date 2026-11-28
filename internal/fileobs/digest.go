package fileobs

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
)

// ErrDigestMismatch means content did not match the digest it was required
// to match. Like ErrConditionMismatch it is its own category: a caller
// verifying integrity must be able to tell "the bytes are wrong" apart
// from "the bytes could not be read" (REQ-CHK-020, REQ-CHK-030).
var ErrDigestMismatch = errors.New("content does not match its expected digest")

// DigestSHA256 is the full-content SHA-256 of everything r yields. The
// digest is meaningless unless the read completes, so a read error is
// never paired with a partial digest.
func DigestSHA256(r io.Reader) ([32]byte, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return [32]byte{}, err
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out, nil
}

// DigestFileSHA256 is the full-content SHA-256 of the file at path.
func DigestFileSHA256(path string) ([32]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return [32]byte{}, err
	}
	defer f.Close()
	return DigestSHA256(f)
}

// VerifyDigest reads r to completion and compares its full-content digest
// with want. The digest is only compared once every byte has been read —
// there is no early-out — so a truncated stream is a digest mismatch
// rather than a success over a prefix, and a mismatch is reported as
// ErrDigestMismatch while a read or write failure keeps its own category
// (REQ-CHK-040). A write failure to sink ends the read early, but that
// failure is terminal for the caller anyway.
//
// When sink is not nil, bytes are written through as they are read,
// before the digest is known. A mismatch leaves whatever already passed
// through in place — this is the documented contract, and it is why
// callers that must not publish unverified bytes buffer or stage first
// and call VerifyDigest on the complete content instead.
func VerifyDigest(r io.Reader, sink io.Writer, want [32]byte) error {
	h := sha256.New()
	tee := io.TeeReader(r, h)
	if sink != nil {
		if _, err := io.Copy(sink, tee); err != nil {
			return err
		}
	} else if _, err := io.Copy(io.Discard, tee); err != nil {
		return err
	}
	var got [32]byte
	copy(got[:], h.Sum(nil))
	if got != want {
		return fmt.Errorf("%w: got %s, want %s", ErrDigestMismatch, HexDigest(got), HexDigest(want))
	}
	return nil
}

// VerifyContentDigest compares complete content with the digest it was
// required to match, before any of it is published. This is the pre-commit
// shape REQ-CHK-020 asks for: verification of a staged whole, not of a
// stream already on its way out.
func VerifyContentDigest(content []byte, want [32]byte) error {
	return VerifyDigest(bytes.NewReader(content), nil, want)
}
