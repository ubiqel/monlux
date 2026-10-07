package lock

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type Lock struct {
	fd int
}

func Acquire() (Lock, error) {
	dir := runtimeDir()
	err := os.MkdirAll(dir, 0o700)
	if err != nil {
		return Lock{}, fmt.Errorf("creating directory path '%s' for lock: %w", dir, err)
	}
	path := filepath.Join(dir, "shine.lock")

	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR, unix.S_IRUSR|unix.S_IWUSR)
	if err != nil {
		return Lock{}, fmt.Errorf("openning fd '%s' for lock: %w", path, err)
	}

	err = unix.Flock(fd, unix.LOCK_EX)
	if err != nil {
		return Lock{}, fmt.Errorf("acquiring lock with flock: %w", err)
	}

	return Lock{fd: fd}, nil
}

func Release(lock Lock) error {
	err := unix.Close(lock.fd)
	if err != nil {
		return fmt.Errorf("closing lock's fd: %w", err)
	}

	return nil
}

func runtimeDir() string {
	base, ok := os.LookupEnv("XDG_RUNTIME_DIR")
	if !ok {
		return "/tmp/shine"
	}
	return filepath.Join(base, "shine")
}
