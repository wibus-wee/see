package app

import (
	"bufio"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

func TestGTKPortalFileChooserAvailable(t *testing.T) {
	t.Run("supports folders", func(t *testing.T) {
		portalTestSessionBus(t, &portalTestProperties{version: 4})
		if !gtkPortalFileChooserAvailable() {
			t.Fatal("FileChooser portal version 4 was not detected")
		}
	})

	t.Run("old version does not support folders", func(t *testing.T) {
		portalTestSessionBus(t, &portalTestProperties{version: 2})
		if gtkPortalFileChooserAvailable() {
			t.Fatal("FileChooser portal version 2 should not enable folder selection")
		}
	})

	t.Run("missing FileChooser interface", func(t *testing.T) {
		portalTestSessionBus(t, nil)
		if gtkPortalFileChooserAvailable() {
			t.Fatal("portal without Properties.Get was accepted")
		}
	})

	t.Run("portal error", func(t *testing.T) {
		portalTestSessionBus(t, &portalTestProperties{errName: "org.freedesktop.DBus.Error.UnknownInterface"})
		if gtkPortalFileChooserAvailable() {
			t.Fatal("portal error was accepted")
		}
	})

	t.Run("bounded timeout", func(t *testing.T) {
		blocked := make(chan struct{})
		started := make(chan struct{})
		t.Cleanup(func() { close(blocked) })
		portalTestSessionBus(t, &portalTestProperties{blocked: blocked, started: started})

		start := time.Now()
		if gtkPortalFileChooserAvailable() {
			t.Fatal("timed-out portal query was accepted")
		}
		elapsed := time.Since(start)
		select {
		case <-started:
		default:
			t.Fatal("portal did not receive the capability query")
		}
		if elapsed < gtkPortalProbeTimeout/2 || elapsed > gtkPortalProbeTimeout+300*time.Millisecond {
			t.Fatalf("probe returned after %v, want a bounded %v timeout", elapsed, gtkPortalProbeTimeout)
		}
	})
}

type portalTestProperties struct {
	version   uint32
	errName   string
	blocked   <-chan struct{}
	started   chan struct{}
	startOnce sync.Once
}

func (p *portalTestProperties) Get(iface, property string) (dbus.Variant, *dbus.Error) {
	if p.started != nil {
		p.startOnce.Do(func() { close(p.started) })
	}
	if p.blocked != nil {
		<-p.blocked
	}
	if p.errName != "" {
		return dbus.Variant{}, dbus.NewError(p.errName, nil)
	}
	if iface != "org.freedesktop.portal.FileChooser" || property != "version" {
		return dbus.Variant{}, dbus.NewError("org.freedesktop.DBus.Error.UnknownProperty", nil)
	}
	return dbus.MakeVariant(p.version), nil
}

func portalTestSessionBus(t *testing.T, properties *portalTestProperties) {
	t.Helper()
	address := startPortalTestBus(t)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", address)

	conn, err := dbus.Connect(address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if properties != nil {
		if err := conn.Export(properties, dbus.ObjectPath("/org/freedesktop/portal/desktop"), "org.freedesktop.DBus.Properties"); err != nil {
			t.Fatal(err)
		}
	}
	reply, err := conn.RequestName(gtkPortalDesktopBusName, dbus.NameFlagDoNotQueue)
	if err != nil {
		t.Fatal(err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("portal bus name reply = %v, want primary owner", reply)
	}
}

func startPortalTestBus(t *testing.T) string {
	t.Helper()
	daemon, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon is unavailable")
	}
	cmd := exec.Command(daemon, "--session", "--nofork", "--print-address=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	address := make(chan string, 1)
	readErr := make(chan error, 1)
	go func() {
		line, err := bufio.NewReader(stdout).ReadString('\n')
		if err != nil {
			readErr <- err
			return
		}
		address <- strings.TrimSpace(line)
	}()
	select {
	case addr := <-address:
		if addr == "" {
			t.Fatal("dbus-daemon returned an empty address")
		}
		return addr
	case err := <-readErr:
		t.Fatalf("read dbus-daemon address: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out starting dbus-daemon")
	}
	return ""
}
