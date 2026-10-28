package nav

import "testing"

func TestBackForward(t *testing.T) {
	var h History
	a, b, c := Entry{"a.go", 1, 0}, Entry{"b.go", 2, 3}, Entry{"c.go", 9, 4}
	h.Push(a) // jumped a → b
	h.Push(b) // jumped b → c
	if to, ok := h.GoBack(c); !ok || to != b {
		t.Fatalf("back = %v %v", to, ok)
	}
	if to, ok := h.GoBack(b); !ok || to != a {
		t.Fatalf("back = %v %v", to, ok)
	}
	if _, ok := h.GoBack(a); ok {
		t.Fatal("back past the start")
	}
	if to, ok := h.GoForward(a); !ok || to != b {
		t.Fatalf("forward = %v %v", to, ok)
	}
	// A new jump clears what is forward.
	h.Push(b)
	if len(h.Forward) != 0 {
		t.Errorf("forward kept %v", h.Forward)
	}
	if len(h.Back) != 2 {
		t.Errorf("duplicate kept: %v", h.Back)
	}
}

func TestLimitAndForget(t *testing.T) {
	h := History{Limit: 3}
	for i := range 10 {
		h.Push(Entry{"f.go", i, 0})
	}
	if len(h.Back) != 3 || h.Back[0].Line != 7 {
		t.Errorf("back = %v", h.Back)
	}
	h.Push(Entry{"g.go", 0, 0})
	h.Forget("f.go")
	if len(h.Back) != 1 || h.Back[0].Path != "g.go" {
		t.Errorf("after forget: %v", h.Back)
	}
}
