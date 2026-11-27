// Package operr classifies operation failures without rendering external messages.
package operr

import (
	"context"
	"errors"
	"io/fs"
	"os/exec"
	"runtime"
	"time"
)

type Kind string
type Status string
type Finality string

const (
	Unexpected       Kind     = "unexpected"
	Unsupported      Kind     = "unsupported"
	InvalidInput     Kind     = "invalid-input"
	NotFound         Kind     = "not-found"
	PermissionDenied Kind     = "permission-denied"
	Conflict         Kind     = "conflict"
	OutOfRange       Kind     = "out-of-range"
	RateLimited      Kind     = "rate-limited"
	ContentMismatch  Kind     = "content-mismatch"
	Timeout          Kind     = "timeout"
	Cancelled        Kind     = "cancelled"
	Temporary        Status   = "temporary"
	Permanent        Status   = "permanent"
	Persistent       Status   = "persistent"
	NoEffect         Finality = "no-effect"
	Uncertain        Finality = "uncertain"
)

type Error struct {
	Kind      Kind
	Status    Status
	Finality  Finality
	Operation string
	Attempt   int
	Duration  time.Duration
	Trace     []uintptr
	cause     error
}

func (e *Error) Error() string { return string(e.Kind) + " (" + string(e.Status) + ")" }
func (e *Error) Unwrap() error { return e.cause }

// New accepts trusted classification constants, never protocol messages.
func New(k Kind, s Status, cause error) *Error {
	e := &Error{Kind: k, Status: s, cause: cause}
	if k == Unexpected {
		pcs := make([]uintptr, 12)
		n := runtime.Callers(2, pcs)
		e.Trace = pcs[:n]
	}
	return e
}
func Classify(err error) *Error {
	if err == nil {
		return nil
	}
	var existing *Error
	if errors.As(err, &existing) {
		c := *existing
		return &c
	}
	switch {
	case errors.Is(err, context.Canceled):
		return New(Cancelled, Permanent, err)
	case errors.Is(err, context.DeadlineExceeded):
		return New(Timeout, Temporary, err)
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, exec.ErrNotFound):
		return New(NotFound, Permanent, err)
	case errors.Is(err, fs.ErrPermission):
		return New(PermissionDenied, Permanent, err)
	case errors.Is(err, fs.ErrExist):
		return New(Conflict, Permanent, err)
	case errors.Is(err, fs.ErrInvalid):
		return New(InvalidInput, Permanent, err)
	}
	return New(Unexpected, Temporary, err)
}
