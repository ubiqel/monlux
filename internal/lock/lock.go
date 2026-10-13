package lock

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type Lock int

func Acquire() (Lock, error) {
	return acquireAt(resolvePath())
}

func acquireAt(path string) (Lock, error) {
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDONLY|unix.O_CLOEXEC, 0o600)
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

func resolvePath() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		// XDG_RUNTIME_DIR is trusted user config, and Stat only checks it exists.
		if info, err := os.Stat(dir); err == nil && info.IsDir() { //nolint:gosec // G703: trusted env var, only checked with Stat
			return filepath.Join(dir, "monlux.lock")
		}
	}

	return filepath.Join(os.TempDir(), fmt.Sprintf("monlux-%d.lock", os.Getuid()))
}

func (l *Lock) Release() error {
	if l == nil || int(*l) < 0 {
		return nil
	}

	fd := int(*l)
	*l = -1

	if err := unix.Close(fd); err != nil {
		return fmt.Errorf("closing lock's fd: %w", err)
	}

	return nil
}
