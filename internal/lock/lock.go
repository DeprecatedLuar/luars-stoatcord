// Package lock provides a flock(2)-based single-instance guard.
package lock

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// ErrLocked is returned by Acquire when another process already holds the lock.
var ErrLocked = errors.New("lock: already held by another process")

// lockFileMode restricts the lock file to the owning user.
const lockFileMode = 0o600

// Lock is an exclusive advisory lock held on a file for the life of a process.
// The kernel releases it automatically if the process dies, so no stale
// lock file can block a restart.
type Lock struct {
	file *os.File
}

// Acquire takes a non-blocking exclusive flock on path, creating it if needed.
// It returns ErrLocked if another live process already holds it.
func Acquire(path string) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, lockFileMode)
	if err != nil {
		return nil, fmt.Errorf("lock: open %s: %w", path, err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("lock: flock %s: %w", path, err)
	}

	return &Lock{file: f}, nil
}

// Release drops the lock and closes the underlying file.
func (l *Lock) Release() error {
	if err := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN); err != nil {
		l.file.Close()
		return fmt.Errorf("lock: unlock: %w", err)
	}
	return l.file.Close()
}
