package auth

import (
	"fmt"
	"os"
	"syscall"
)

type FileLock struct{ f *os.File }

func AcquireLock(path string) (*FileLock, error) {
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // path is caller-controlled
	if err != nil {
		return nil, fmt.Errorf("auth: open lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil { //nolint:gosec // uintptr->int safe on all supported platforms
		_ = f.Close()
		return nil, fmt.Errorf("auth: flock: %w", err)
	}
	return &FileLock{f: f}, nil
}

func (l *FileLock) Release() error {
	if err := syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN); err != nil { //nolint:gosec // uintptr->int safe on all supported platforms
		return err
	}
	return l.f.Close()
}
