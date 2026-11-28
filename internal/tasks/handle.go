package tasks

import (
	"context"
	"os/exec"
	"time"

	"see/internal/operr"
	"see/internal/proc"
)

type CommandFactory func(context.Context, string, ...string) *exec.Cmd

// Handle is a value snapshot. Order: admission, total timeout (caller context),
// retry, per-attempt timeout, pre-start hook, process. Only start failures can
// be replayed: neither go test nor go build is assumed side-effect free.
type Handle struct {
	now     func() time.Time
	command CommandFactory
	policy  Policy
	slots   chan struct{}
}

// BeforeStart runs before side effects and must honor its context. Observe runs
// after each attempt, including the final persistent failure. Both may be
// called concurrently by different dispatches. Admission fails without queuing.
type Policy struct {
	Attempts         int
	Timeout, Backoff time.Duration
	Concurrent       int
	BeforeStart      func(context.Context, int) error
	Observe          func(attempt int, failure *operr.Error)
}

func (h Handle) WithClock(now func() time.Time) Handle { h.now = now; return h }

func (h Handle) WithCommand(f CommandFactory) Handle { h.command = f; return h }

// WithPolicy replaces the whole policy. Zero means no limit or a single attempt.
func (h Handle) WithPolicy(p Policy) (Handle, error) {
	if p.Attempts < 0 || p.Timeout < 0 || p.Backoff < 0 || p.Concurrent < 0 {
		return h, operr.New(operr.InvalidInput, operr.Permanent, nil)
	}
	h.policy = p
	h.slots = nil
	if p.Concurrent > 0 {
		h.slots = make(chan struct{}, p.Concurrent)
	}
	return h, nil
}
func (h Handle) Run(ctx context.Context, exe, root string, k Kind, out *Output) error {
	now := h.now
	if now == nil {
		now = time.Now
	}
	start := now()
	observe := func(n int, e *operr.Error) {
		if h.policy.Observe != nil {
			if e != nil {
				snapshot := *e
				h.policy.Observe(n, &snapshot)
			} else {
				h.policy.Observe(n, nil)
			}
		}
	}
	op := "go-task"
	fail := func(err error, n int, f operr.Finality) *operr.Error {
		e := operr.Classify(err)
		e.Attempt = n
		e.Duration = now().Sub(start)
		e.Operation = op
		e.Finality = f
		return e
	}
	args := k.Args()
	if args == nil || out == nil {
		return fail(operr.New(operr.InvalidInput, operr.Permanent, nil), 0, operr.NoEffect)
	}
	op = "go-" + string(k)
	if err := ctx.Err(); err != nil {
		return fail(err, 0, operr.NoEffect)
	}
	if h.slots != nil {
		select {
		case h.slots <- struct{}{}:
			defer func() { <-h.slots }()
		default:
			return fail(operr.New(operr.RateLimited, operr.Temporary, nil), 0, operr.NoEffect)
		}
	}
	factory := h.command
	if factory == nil {
		factory = proc.CommandContext
	}
	for n := 1; ; n++ {
		if err := ctx.Err(); err != nil {
			return fail(err, n-1, operr.NoEffect)
		}
		a := ctx
		cancel := func() {}
		if h.policy.Timeout > 0 {
			a, cancel = context.WithTimeout(ctx, h.policy.Timeout)
		}
		var err error
		if hook := h.policy.BeforeStart; hook != nil {
			err = hook(a, n)
		}
		started := false
		if err == nil && a.Err() == nil {
			cmd := factory(a, exe, args...)
			cmd.Dir = root
			cmd.Stdout, cmd.Stderr = out, out
			cmd.WaitDelay = 2 * time.Second
			configureProcess(cmd)
			err = cmd.Start()
			if err == nil {
				started = true
				err = cmd.Wait()
			}
		}
		if (!started || err != nil) && a.Err() != nil {
			err = a.Err()
		}
		cancel()
		if err == nil {
			observe(n, nil)
			return nil
		}
		if started {
			e := fail(err, n, operr.Uncertain)
			observe(n, e)
			return e
		}
		e := fail(err, n, operr.NoEffect)
		if ctx.Err() != nil {
			return fail(ctx.Err(), n, operr.NoEffect)
		}
		if e.Status != operr.Temporary || h.policy.Attempts == 0 || (e.Kind != operr.Unexpected && e.Kind != operr.Timeout && e.Kind != operr.RateLimited) {
			observe(n, e)
			return e
		}
		if n >= h.policy.Attempts {
			e.Status = operr.Persistent
			observe(n, e)
			return e
		}
		observe(n, e)
		timer := time.NewTimer(h.policy.Backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fail(ctx.Err(), n, operr.NoEffect)
		case <-timer.C:
		}
	}
}
