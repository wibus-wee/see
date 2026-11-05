package app

import (
	"reflect"
	"testing"
)

func TestDockRecentProjects(t *testing.T) {
	if items := dockRecentItems(nil, nil); len(items) != 0 {
		t.Fatalf("empty recent projects: %v", items)
	}
	paths := []string{"/work/first", "/other/second"}
	var opened []string
	items := dockRecentItems(paths, func(path string) { opened = append(opened, path) })
	if len(items) != 3 || !items[0].Disabled || items[1].Label != "first" || items[2].ToolTip != paths[1] {
		t.Fatalf("unexpected recent project menu: %+v", items)
	}
	items[2].Click(nil, nil)
	items[1].Click(nil, nil)
	if !reflect.DeepEqual(opened, []string{paths[1], paths[0]}) {
		t.Fatalf("opened %v", opened)
	}
}
