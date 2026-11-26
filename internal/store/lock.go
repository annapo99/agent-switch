package store

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

var ErrBusy = errors.New("another ags command is running; try again")

func (s *Store) Lock(wait time.Duration) (func(), error) {
	dir := filepath.Join(s.home, ".agent-switch")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(wait)
	for {
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() {
				_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
				_ = file.Close()
			}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) || !time.Now().Before(deadline) {
			_ = file.Close()
			if errors.Is(err, syscall.EWOULDBLOCK) {
				return nil, ErrBusy
			}
			return nil, err
		}
		time.Sleep(100 * time.Millisecond)
	}
}
