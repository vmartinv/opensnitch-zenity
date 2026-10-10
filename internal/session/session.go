// Package session decides whether a dialog can be shown right now.
package session

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/godbus/dbus/v5"
)

type Checker struct {
	// LockedCommand, if set, is run via sh -c; exit status 0 means locked.
	// Useful for screen lockers that do not set logind's LockedHint.
	LockedCommand string
}

// Usable returns nil if a prompt can be shown, or the reason it cannot.
func (c *Checker) Usable(ctx context.Context) error {
	if os.Getenv("WAYLAND_DISPLAY") == "" && os.Getenv("DISPLAY") == "" {
		return fmt.Errorf("no WAYLAND_DISPLAY or DISPLAY")
	}
	if active, locked, err := graphicalSession(ctx); err == nil {
		if !active {
			return fmt.Errorf("graphical session is not in the foreground")
		}
		if locked {
			return fmt.Errorf("session is locked")
		}
	}
	if c.LockedCommand != "" {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if exec.CommandContext(ctx, "sh", "-c", c.LockedCommand).Run() == nil {
			return fmt.Errorf("locked_check_command reports locked")
		}
	}
	return nil
}

// graphicalSession reports the state of the calling user's display session.
func graphicalSession(ctx context.Context) (active, locked bool, err error) {
	bus, err := dbus.SystemBus() // shared connection, must not be closed
	if err != nil {
		return false, false, err
	}
	var display struct {
		ID   string
		Path dbus.ObjectPath
	}
	user := bus.Object("org.freedesktop.login1", "/org/freedesktop/login1/user/self")
	if err := user.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0,
		"org.freedesktop.login1.User", "Display").Store(&display); err != nil {
		return false, false, err
	}
	if display.Path == "/" {
		return false, false, nil
	}
	sess := bus.Object("org.freedesktop.login1", display.Path)
	if err := sess.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0,
		"org.freedesktop.login1.Session", "Active").Store(&active); err != nil {
		return false, false, err
	}
	if err := sess.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0,
		"org.freedesktop.login1.Session", "LockedHint").Store(&locked); err != nil {
		return false, false, err
	}
	return active, locked, nil
}
