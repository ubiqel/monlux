package lock

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type Lock int

func Acquire() (Lock, error) {
	path := filepath.Join(os.TempDir(), "shine.lock")
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDONLY|unix.O_CLOEXEC, 0o666)
	if err != nil {
		return 0, fmt.Errorf("openning fd '%s' for lock: %w", path, err)
	}

	err = unix.Flock(fd, unix.LOCK_EX)
	if err != nil {
		_ = unix.Close(fd)
		return 0, fmt.Errorf("acquiring lock with flock: %w", err)
	}

	return Lock(fd), nil
}

func (l *Lock) Release() error {
	if l == nil || int(*l) < 0 {
		return nil
	}

	fd := int(*l)
	*l = -1 // Mark as released.

	if err := unix.Close(fd); err != nil {
		return fmt.Errorf("closing lock's fd: %w", err)
	}

	return nil
}
