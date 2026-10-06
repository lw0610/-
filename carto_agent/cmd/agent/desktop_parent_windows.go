//go:build windows

package main

import (
	"errors"
	"os"
	"syscall"
)

func openDesktopParentProbe(parentPID int) (func() bool, func(), error) {
	if os.Getppid() != parentPID {
		return func() bool { return false }, func() {}, nil
	}
	// Keep the original process handle: a reused PID must not keep an orphan alive.
	handle, err := syscall.OpenProcess(syscall.SYNCHRONIZE, false, uint32(parentPID))
	if errors.Is(err, syscall.Errno(87)) { // ERROR_INVALID_PARAMETER: parent has exited.
		return func() bool { return false }, func() {}, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return func() bool {
		state, waitErr := syscall.WaitForSingleObject(handle, 0)
		return waitErr == nil && state == syscall.WAIT_TIMEOUT
	}, func() { _ = syscall.CloseHandle(handle) }, nil
}
