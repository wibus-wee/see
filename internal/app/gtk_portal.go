package app

import (
	"context"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	gtkPortalDesktopBusName        = "org.freedesktop.portal.Desktop"
	gtkPortalProbeTimeout          = 500 * time.Millisecond
	gtkPortalFileChooserMinVersion = 3
)

func gtkPortalFileChooserAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), gtkPortalProbeTimeout)
	defer cancel()

	result := make(chan bool, 1)
	go func() {
		result <- queryGTKPortalFileChooser(ctx)
	}()

	select {
	case available := <-result:
		return ctx.Err() == nil && available
	case <-ctx.Done():
		return false
	}
}

func queryGTKPortalFileChooser(ctx context.Context) bool {
	conn, err := dbus.SessionBusPrivateNoAutoStartup(dbus.WithContext(ctx))
	if err != nil {
		return false
	}
	stopClose := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopClose()
	defer conn.Close()
	if ctx.Err() != nil {
		return false
	}
	if err := conn.Auth(nil); err != nil {
		return false
	}
	if err := conn.Hello(); err != nil {
		return false
	}

	portal := conn.Object(gtkPortalDesktopBusName, "/org/freedesktop/portal/desktop")
	var variant dbus.Variant
	if err := portal.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, "org.freedesktop.portal.FileChooser", "version").Store(&variant); err != nil {
		return false
	}
	version, ok := variant.Value().(uint32)
	return ok && version >= gtkPortalFileChooserMinVersion
}
