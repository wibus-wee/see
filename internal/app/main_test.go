package app

import (
	"os"
	"testing"
)

func TestSetGTKUsePortalDefault(t *testing.T) {
	t.Run("unset with available portal", func(t *testing.T) {
		unsetGTKUsePortal(t)
		if err := setGTKUsePortalDefault(func() bool { return true }); err != nil {
			t.Fatal(err)
		}
		if got, ok := os.LookupEnv(gtkUsePortalEnv); !ok || got != "1" {
			t.Fatalf("GTK_USE_PORTAL = %q, set = %v, want \"1\"", got, ok)
		}
	})

	t.Run("unset without portal", func(t *testing.T) {
		unsetGTKUsePortal(t)
		if err := setGTKUsePortalDefault(func() bool { return false }); err != nil {
			t.Fatal(err)
		}
		if _, ok := os.LookupEnv(gtkUsePortalEnv); ok {
			t.Fatalf("GTK_USE_PORTAL was set without an available portal")
		}
	})

	for _, tc := range []struct {
		name  string
		value string
	}{
		{name: "enabled", value: "1"},
		{name: "disabled", value: "0"},
		{name: "empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(gtkUsePortalEnv, tc.value)
			called := false
			if err := setGTKUsePortalDefault(func() bool { called = true; return true }); err != nil {
				t.Fatal(err)
			}
			if called {
				t.Fatal("portal availability was checked despite an explicit value")
			}
			if got, ok := os.LookupEnv(gtkUsePortalEnv); !ok || got != tc.value {
				t.Fatalf("GTK_USE_PORTAL = %q, set = %v, want %q", got, ok, tc.value)
			}
		})
	}
}

func unsetGTKUsePortal(t *testing.T) {
	t.Helper()
	previous, wasSet := os.LookupEnv(gtkUsePortalEnv)
	if err := os.Unsetenv(gtkUsePortalEnv); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if wasSet {
			_ = os.Setenv(gtkUsePortalEnv, previous)
		} else {
			_ = os.Unsetenv(gtkUsePortalEnv)
		}
	})
}
