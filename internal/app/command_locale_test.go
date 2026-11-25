package app

import (
	"see/locales"
	"strings"
	"testing"
)

func TestCommandTranslations(t *testing.T) {
	for _, cmd := range commands() {
		key := commandMessages[cmd.id]
		if key == "" {
			// Numbered tab commands are generated dynamically.
			if strings.HasPrefix(cmd.id, "tab.select") && cmd.id != "tab.selectLast" {
				continue
			}
			t.Errorf("missing translation for %q", cmd.id)
			continue
		}
		if got := locales.Catalog.Format("en", key, nil); got != cmd.name {
			t.Errorf("%q has English label %q", cmd.id, got)
		}
		if got := locales.Catalog.Format("zh-Hans", key, nil); got == cmd.name {
			t.Errorf("%q has no Chinese label", cmd.id)
		}
	}
}
