//go:build !windows

package main

import "os"

func openDesktopParentProbe(parentPID int) (func() bool, func(), error) {
	// Unix reparents an orphan when its desktop parent exits, including SIGKILL.
	return func() bool { return os.Getppid() == parentPID }, func() {}, nil
}
