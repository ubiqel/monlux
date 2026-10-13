package lock

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/matryer/is"
)

func TestPathUsesRuntimeDir(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	is := is.New(t)

	is.Equal(resolvePath(), filepath.Join(runtimeDir, "monlux.lock"))
}

func TestPathFallsBackWhenRuntimeDirMissing(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(t.TempDir(), "missing"))
	is := is.New(t)

	is.Equal(resolvePath(), fallbackPath())
}

func TestPathFallsBackWhenRuntimeDirUnset(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	is := is.New(t)

	is.Equal(resolvePath(), fallbackPath())
}

func TestAcquireCreatesOwnerOnlyFile(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "monlux.lock")
	is := is.New(t)

	l, err := acquireAt(lockPath)
	is.NoErr(err)
	is.NoErr(l.Release())

	info, err := os.Stat(lockPath)
	is.NoErr(err)
	is.Equal(info.Mode().Perm(), os.FileMode(0o600))
}

func TestReleaseIsIdempotent(t *testing.T) {
	is := is.New(t)

	l, err := acquireAt(filepath.Join(t.TempDir(), "monlux.lock"))
	is.NoErr(err)
	is.NoErr(l.Release())
	is.NoErr(l.Release())
}

func fallbackPath() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("monlux-%d.lock", os.Getuid()))
}
