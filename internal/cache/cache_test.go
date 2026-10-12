package cache

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/matryer/is"
)

func isolateCache(t *testing.T) {
	t.Helper()

	t.Setenv("XDG_CACHE_HOME", t.TempDir())
}

type sample struct {
	Name string `json:"name"`
}

func TestWriteThenReadJSON(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	is.NoErr(WriteJSON("sample.json", sample{Name: "x"}))

	var got sample
	is.NoErr(ReadJSON("sample.json", &got))
	is.Equal(got, sample{Name: "x"})
}

func TestReadJSONMissing(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	var got sample
	is.True(errors.Is(ReadJSON("missing.json", &got), ErrNotExist))
}

func TestReadJSONCorruptIsRemoved(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	path, err := Path("corrupt.json")
	is.NoErr(err)
	is.NoErr(os.WriteFile(path, []byte("{not json"), 0o644))

	var got sample
	is.True(errors.Is(ReadJSON("corrupt.json", &got), ErrNotExist))

	_, err = os.Stat(filepath.Clean(path))
	is.True(os.IsNotExist(err))
}

func TestPathIsInAppCacheDir(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	path, err := Path("x.json")
	is.NoErr(err)
	is.Equal(filepath.Base(filepath.Dir(path)), appDirName)
}
