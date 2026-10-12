package lock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type Lock int

func Acquire() (Lock, error) {
	// We use temp dir instead of XDG_RUNTIME_DIR to handle the situation
	// in which our app is running under multiple users at the same time.
	path := filepath.Join(os.TempDir(), "monlux.lock")
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDONLY|unix.O_CLOEXEC, 0o666)
	if err != nil {
		return 0, fmt.Errorf("openning fd '%s' for lock: %w", path, err)
	}

	// The umask would otherwise strip group/other permissions from a new file,
	// and users sharing the lock need all of them. Only the owner may chmod, so
	// EPERM just means another user created the file first.
	if err := unix.Fchmod(fd, 0o666); err != nil && !errors.Is(err, unix.EPERM) {
		_ = unix.Close(fd)

		return 0, fmt.Errorf("setting lock file mode: %w", err)
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
