package app

import (
	"see/locales"
	"testing"
)

func TestCommandTranslations(t *testing.T) {
	for _, cmd := range commands() {
		key := commandMessages[cmd.name]
		if key == "" {
			// Numbered tab commands are generated dynamically.
			if len(cmd.name) >= len("Select Tab ") && cmd.name[:len("Select Tab ")] == "Select Tab " {
				continue
			}
			t.Errorf("missing translation for %q", cmd.name)
			continue
		}
		if got := locales.Catalog.Format("en", key, nil); got != cmd.name {
			t.Errorf("%q has English label %q", cmd.name, got)
		}
		if got := locales.Catalog.Format("zh-Hans", key, nil); got == cmd.name {
			t.Errorf("%q has no Chinese label", cmd.name)
		}
	}
}
