package provider

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	claudeLockStaleness     = 10 * time.Second
	claudeLockTouchInterval = 3 * time.Second
	claudeLockTimeout       = 9 * time.Second
)

func withClaudeLocks(home string, fn func() error) error {
	return withProperLock(filepath.Join(home, ".claude.lock"), func() error {
		return withProperLock(filepath.Join(home, ".claude.json.lock"), fn)
	})
}

func withProperLock(lockDir string, fn func() error) error {
	if err := acquireProperLock(lockDir); err != nil {
		return err
	}
	done := make(chan struct{})
	go touchLockUntilDone(lockDir, done)
	defer func() {
		close(done)
		_ = os.Remove(lockDir)
	}()
	return fn()
}

func acquireProperLock(lockDir string) error {
	if err := os.MkdirAll(filepath.Dir(lockDir), 0o755); err != nil {
		return err
	}
	start := time.Now()
	for {
		if err := os.Mkdir(lockDir, 0o755); err == nil {
			return nil
		} else if !os.IsExist(err) {
			return err
		}
		if time.Since(start) > claudeLockTimeout {
			return fmt.Errorf("could not acquire %s; Claude Code may be refreshing credentials", filepath.Base(lockDir))
		}
		info, err := os.Stat(lockDir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if time.Since(info.ModTime()) > claudeLockStaleness {
			_ = os.Remove(lockDir)
			continue
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func touchLockUntilDone(lockDir string, done <-chan struct{}) {
	ticker := time.NewTicker(claudeLockTouchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			_ = os.Chtimes(lockDir, time.Now(), time.Now())
		}
	}
}
