//go:build darwin

package app

import (
	"runtime"
	"testing"
)

type appKitTestMainCall struct {
	run    func() error
	result chan error
}

var appKitTestMainCalls = make(chan appKitTestMainCall)

func runOnAppKitTestMainThread(fn func() error) error {
	call := appKitTestMainCall{run: fn, result: make(chan error, 1)}
	appKitTestMainCalls <- call
	return <-call.result
}

func runTestsWithAppKitMainThread(m *testing.M) int {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	done := make(chan int, 1)
	go func() { done <- m.Run() }()
	for {
		select {
		case call := <-appKitTestMainCalls:
			call.result <- call.run()
		case code := <-done:
			return code
		}
	}
}
