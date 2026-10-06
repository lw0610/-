package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const desktopParentPIDEnv = "AUTOCARTO_DESKTOP_PARENT_PID"

type parentProbeFactory func(int) (alive func() bool, closeProbe func(), err error)

// A nil channel leaves standalone agents governed only by their normal signals.
func watchDesktopParent(ctx context.Context, rawPID string, interval time.Duration) (<-chan struct{}, error) {
	return watchDesktopParentWithProbe(ctx, rawPID, interval, openDesktopParentProbe)
}

func watchDesktopParentWithProbe(ctx context.Context, rawPID string, interval time.Duration, openProbe parentProbeFactory) (<-chan struct{}, error) {
	rawPID = strings.TrimSpace(rawPID)
	if rawPID == "" {
		return nil, nil
	}
	parentPID, err := strconv.Atoi(rawPID)
	if err != nil || parentPID <= 1 {
		return nil, fmt.Errorf("%s must identify the desktop parent process", desktopParentPIDEnv)
	}
	alive, closeProbe, err := openProbe(parentPID)
	if err != nil {
		return nil, fmt.Errorf("monitor desktop parent: %w", err)
	}
	if interval <= 0 {
		interval = time.Second
	}
	gone := make(chan struct{})
	if !alive() {
		closeProbe()
		close(gone)
		return gone, nil
	}
	go func() {
		defer closeProbe()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			if !alive() {
				close(gone)
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return gone, nil
}

// A newer backend can have published a different port while this process exits.
func removeOwnedRuntimePortFile(path string, port int) error {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(raw)) != strconv.Itoa(port) {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
