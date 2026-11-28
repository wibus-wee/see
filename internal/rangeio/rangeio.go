// Package rangeio reads windows of local files under one pinned version,
// so a view built of many reads never mixes bytes of different versions.
//
// The design follows the range-read semantics of Apache OpenDAL: a range
// that extends past the end of the file is not an error but a short read
// reported through an end-of-file flag, while reading a version that no
// longer exists is. Where OpenDAL pins a version by ETag and conditional
// requests, a local file has no snapshots, so a Session pins the size and
// modification time of the file and fails with ErrChanged when either one
// differs, which is the only signal a local filesystem reliably offers.
//
// A Session holds no resources between calls: every read opens, reads and
// closes the file, so a canceled or abandoned operation leaves nothing
// behind but what the operating system reclaims with the file descriptor.
package rangeio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

// ErrChanged reports that the file no longer matches the version a
// Session is pinned to. Data of a read that returns ErrChanged is
// discarded, never delivered partially.
var ErrChanged = errors.New("file changed on disk")

// Stamp identifies one version of a file by the metadata a local
// filesystem reports. Two reads of one file share its content version as
// long as size and modification time both stay the same. Any replacement
// that preserves both escapes detection, however the bytes change: a
// write in place that restores the modification time, or a rename of a
// crafted copy over the file as rsync -t and tar extraction do, the
// usual shape of archive and sync tooling. Detection also leans on the
// filesystem's timestamp granularity: where modification time resolves
// coarsely (seconds on some filesystems), even unwitting quick rewrites
// can land inside one tick.
type Stamp struct {
	Size    int64
	ModTime time.Time
}

// StampOf takes the stamp of the version info describes.
func StampOf(info os.FileInfo) Stamp {
	return Stamp{Size: info.Size(), ModTime: info.ModTime()}
}

// concurrency caps the range operations in flight process-wide, so many
// views reading many windows at once cannot exhaust file descriptors or
// memory: in-flight memory stays below concurrency times the largest
// window any caller requests.
const concurrency = 8

// Limiter serializes range operations up to its capacity. The zero-value
// ready-made global limiter covers the sessions Open creates; tests and
// callers that need their own ceiling construct one with NewLimiter.
type Limiter struct {
	sem chan struct{}
}

// NewLimiter returns a Limiter that admits at most n operations at once.
func NewLimiter(n int) *Limiter {
	if n < 1 {
		n = 1
	}
	return &Limiter{sem: make(chan struct{}, n)}
}

// acquire blocks until a slot is free or ctx is done. The wait is
// interruptible; an operation already reading is not.
func (l *Limiter) acquire(ctx context.Context) error {
	select {
	case l.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (l *Limiter) release() {
	<-l.sem
}

var process = NewLimiter(concurrency)

// Session reads windows of one version of one file. It is a value of
// pinned metadata, not a handle: nothing is held open between calls and
// there is nothing to close.
type Session struct {
	path    string
	stamp   Stamp
	limiter *Limiter
}

// Open pins a session to the version of path as it is right now.
func Open(path string) (*Session, error) {
	return OpenWithLimiter(path, process)
}

// OpenWithLimiter pins a session like Open, sharing the given limiter
// instead of the process-wide one.
func OpenWithLimiter(path string, l *Limiter) (*Session, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s is a directory", path)
	}
	return &Session{path: path, stamp: StampOf(info), limiter: l}, nil
}

// Size is the size of the pinned version.
func (s *Session) Size() int64 {
	return s.stamp.Size
}

// Stamp is the version the session reads.
func (s *Session) Stamp() Stamp {
	return s.stamp
}

// ScanChunk is the default window Scan reads at once when the caller does
// not ask for another size.
const ScanChunk = 256 << 10

// ReadAt reads up to length bytes of the pinned version starting at off.
//
// A range that extends past the end of the file yields a short read, not
// an error: eof reports whether the returned bytes run to the end of the
// pinned version, and a range that starts at or past it yields no bytes
// with eof set. A length of zero performs no I/O and answers from the
// pinned metadata alone.
//
// The read fails with ErrChanged when the file's size or modification
// time differs from the pinned version at any point of the read, and no
// bytes of the mismatched version are returned. Cancellation interrupts
// the wait for a slot but not the read itself, which is bounded by length
// and thereby short; ctx being done before the read starts reports the
// context's error without touching the file.
func (s *Session) ReadAt(ctx context.Context, off, length int64) (data []byte, eof bool, err error) {
	if off < 0 || length < 0 {
		return nil, false, fmt.Errorf("rangeio: negative offset %d or length %d", off, length)
	}
	size := s.stamp.Size
	if off >= size {
		return nil, true, nil
	}
	if length == 0 {
		return nil, false, nil
	}
	if length > size-off {
		length = size - off
	}
	if err := s.limiter.acquire(ctx); err != nil {
		return nil, false, err
	}
	defer s.limiter.release()
	f, err := os.Open(s.path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	if changed(f, s.stamp) {
		return nil, false, ErrChanged
	}
	data = make([]byte, length)
	n, readErr := readFullAt(f, data, off)
	data = data[:n]
	if changed(f, s.stamp) {
		return nil, false, ErrChanged
	}
	if readErr != nil {
		// The version still matches, so a short read means the
		// filesystem itself cannot deliver bytes of a regular file
		// it reports as present.
		return nil, false, readErr
	}
	return data, off+int64(len(data)) >= size, nil
}

// Scan streams the pinned version from start to its end in order, handing
// each chunk of at most chunk bytes to yield. The chunk slice is reused
// between calls and must not be retained. Returning false from yield
// stops the scan early without an error. start past the end scans
// nothing; a chunk size of zero or less reads ScanChunk bytes at a time.
//
// The version is verified before every chunk, so bytes of two versions
// are never mixed into one scan: a change in place fails the scan with
// ErrChanged and discards whatever chunk was in flight.
func (s *Session) Scan(ctx context.Context, start, chunk int64, yield func(off int64, data []byte) bool) error {
	if start < 0 {
		return fmt.Errorf("rangeio: negative start %d", start)
	}
	if chunk <= 0 {
		chunk = ScanChunk
	}
	size := s.stamp.Size
	if start >= size {
		return nil
	}
	if err := s.limiter.acquire(ctx); err != nil {
		return err
	}
	defer s.limiter.release()
	f, err := os.Open(s.path)
	if err != nil {
		return err
	}
	defer f.Close()
	if changed(f, s.stamp) {
		return ErrChanged
	}
	buf := make([]byte, chunk)
	for off := start; off < size; {
		if err := ctx.Err(); err != nil {
			return err
		}
		want := chunk
		if want > size-off {
			want = size - off
		}
		n, readErr := readFullAt(f, buf[:want], off)
		// The version is verified after every read and before its
		// bytes are handed on, because yield delivers chunks
		// incrementally: a change detected only at the end of the
		// scan would already have mixed versions into the stream.
		// A change landing inside one read itself is the window no
		// filesystem check can close.
		if changed(f, s.stamp) {
			return ErrChanged
		}
		if n > 0 {
			if !yield(off, buf[:n]) {
				return nil
			}
		}
		off += int64(n)
		if readErr != nil {
			return readErr
		}
	}
	return nil
}

// readFullAt reads len(buf) bytes at off, tolerating the short reads a
// positioned read may legitimately return. io.EOF at the boundary is
// consumed silently when buf was filled to the end of what exists.
func readFullAt(f *os.File, buf []byte, off int64) (int, error) {
	n := 0
	for n < len(buf) {
		m, err := f.ReadAt(buf[n:], off+int64(n))
		n += m
		if err == io.EOF {
			if n == len(buf) {
				return n, nil
			}
			return n, io.ErrUnexpectedEOF
		}
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// changed reports whether the open file no longer matches the stamp.
func changed(f *os.File, stamp Stamp) bool {
	info, err := f.Stat()
	if err != nil {
		return true
	}
	return info.Size() != stamp.Size || !info.ModTime().Equal(stamp.ModTime)
}
