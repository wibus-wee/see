// Verification probes written by the independent verifier (verify-lane-a):
// the matrix claims "actual subprocess exit classified conservatively
// with uncertain effects". Probe what a user-facing classification of an
// ordinary nonzero exit is.

package tasks

import (
	"context"
	"os"
	"testing"

	"see/internal/operr"
)

// A go build that exits nonzero (a compile error) is permanent in every
// meaningful sense: the same inputs cannot succeed. The spec's conservative
// mapping rule (REQ-ERR-100) excuses "unexpected/temporary" for unknown
// failures, but an exit code is not unknown to a process boundary: it is
// the one failure shape every child has. Pin what the classification
// actually is, so the matrix's claim can be judged.
func TestVerifySubprocessExitClassification(t *testing.T) {
	t.Setenv("SEE_TASK_HELPER", "fail")
	exe, err := testExecutable()
	if err != nil {
		t.Fatal(err)
	}
	err = Run(context.Background(), exe, t.TempDir(), Build, &Output{})
	e := operr.Classify(err)
	if e == nil {
		t.Fatal("exit error not classified")
	}
	t.Logf("nonzero child exit classifies as: kind=%s status=%s finality=%s", e.Kind, e.Status, e.Finality)
	if e.Finality != operr.Uncertain {
		t.Fatalf("a started process's failure must be uncertain: %+v", e)
	}
	if e.Status != operr.Temporary {
		t.Fatalf("matrix claim broken: exit failure is not conservative-temporary: %+v", e)
	}
	if e.Kind != operr.Unexpected {
		t.Fatalf("matrix claim broken: exit failure is not unexpected: %+v", e)
	}
}

func testExecutable() (string, error) {
	return os.Executable()
}
