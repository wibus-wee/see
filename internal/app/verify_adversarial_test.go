// Verification probes written by the independent verifier (verify-lane-a):
// the cross-lane question of whether task-output truncation is visible,
// and the unsupported-operation presentation the audit claims.

package app

import (
	"context"
	"strings"
	"testing"

	"see/internal/tasks"
)

// Lane D's report assigns terminal-output truncation visibility to Lane
// A's internal/tasks. The UI wiring exists in taskView; this test renders
// the panel against output that actually overflowed and checks the user
// is told bytes were dropped.
func TestVerifyTaskTruncationIsVisible(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.txt": "hello"}))
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	run := &taskRun{kind: tasks.Build, output: &tasks.Output{}, cancel: cancel, status: "taskFailed"}
	h.a.task = run
	if _, err := run.output.Write([]byte(strings.Repeat("x", tasks.MaxOutput+4096))); err != nil {
		t.Fatal(err)
	}
	if _, truncated := run.output.Snapshot(); !truncated {
		t.Fatal("control failed: oversized output not flagged truncated")
	}
	h.a.showTasks()
	h.tt.Frame()
	if !h.tt.HasText("Earlier output was removed") {
		t.Fatalf("truncation invisible in the task panel: %v", h.tt.Texts())
	}
}
