package brightness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/matryer/is"
	"github.com/ubiqel/monlux/internal/cache"
)

func isolateCache(t *testing.T) {
	t.Helper()

	t.Setenv("XDG_CACHE_HOME", t.TempDir())
}

func TestEntryFresh(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name    string
		updated time.Time
		want    bool
	}{
		{"just updated", now, true},
		{"within ttl", now.Add(-4 * time.Second), true},
		{"exactly ttl", now.Add(-TTL), true},
		{"one nanosecond past ttl", now.Add(-TTL - time.Nanosecond), false},
		{"expired", now.Add(-6 * time.Second), false},
		{"updated in the future", now.Add(time.Second), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			is.Equal(Entry{UpdatedAt: tt.updated}.Fresh(now), tt.want)
		})
	}
}

func TestStoreRoundTrip(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	Store(4, "A", 40, 100)
	Store(5, "B", 60, 200)

	is.Equal(len(LoadCache()), 2)

	a, ok := Lookup(4, "A")
	is.True(ok)
	is.Equal(a.Current, uint16(40))
	is.Equal(a.Maximum, uint16(100))

	b, ok := Lookup(5, "B")
	is.True(ok)
	is.Equal(b.Current, uint16(60))
	is.Equal(b.Maximum, uint16(200))
}

func TestStoreReplugResetsEntry(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	Store(4, "OLD", 40, 100)
	Store(4, "NEW", 10, 100)

	e, ok := Lookup(4, "NEW")
	is.True(ok)
	is.Equal(e.Current, uint16(10))
}

func TestLookupRejectsOtherIdentity(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	Store(4, "A", 40, 100)

	_, ok := Lookup(4, "B")
	is.True(!ok)
}

func TestForgetRemovesEntry(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	Store(4, "A", 40, 100)
	Forget(4)

	_, ok := Lookup(4, "A")
	is.True(!ok)
}

func TestForgetAbsentBusIsNoop(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	Store(4, "A", 40, 100)
	Forget(9)

	_, ok := Lookup(4, "A")
	is.True(ok)
}

func TestForgetKeepsOtherBuses(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	Store(4, "A", 40, 100)
	Store(5, "B", 60, 200)
	Forget(4)

	_, ok := Lookup(4, "A")
	is.True(!ok)

	b, ok := Lookup(5, "B")
	is.True(ok)
	is.Equal(b.Current, uint16(60))
}

func TestLookupMissingBus(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	Store(4, "A", 40, 100)

	_, ok := Lookup(5, "A")
	is.True(!ok)
}

func TestLookupRejectsZeroMaximum(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	Store(4, "A", 0, 0)

	_, ok := Lookup(4, "A")
	is.True(!ok)
}

func TestStoreSameIdentityUpdatesEntry(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	Store(4, "A", 40, 100)
	first, ok := Lookup(4, "A")
	is.True(ok)

	Store(4, "A", 50, 200)

	e, ok := Lookup(4, "A")
	is.True(ok)
	is.Equal(e.Current, uint16(50))
	is.Equal(e.Maximum, uint16(200))
	is.True(!e.UpdatedAt.Before(first.UpdatedAt))
}

func TestLoadCacheMissingFileIsEmpty(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	c := LoadCache()
	is.True(c != nil)
	is.Equal(len(c), 0)
}

func TestLoadCacheCorruptFileIsIgnored(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	writeCacheFile(t, []byte("{not json"))
	is.Equal(len(LoadCache()), 0)

	Store(4, "A", 40, 100)

	_, ok := Lookup(4, "A")
	is.True(ok)
}

func TestStoreAfterNullCacheFile(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	// json.Unmarshal leaves a map nil for "null", and writing to it would panic.
	writeCacheFile(t, []byte("null"))

	Store(4, "A", 40, 100)

	_, ok := Lookup(4, "A")
	is.True(ok)
}

func TestStoreUnwritableCacheDir(t *testing.T) {
	// A regular file where the cache dir should be makes every write fail.
	blocker := filepath.Join(t.TempDir(), "blocker")
	is := is.New(t)
	is.NoErr(os.WriteFile(blocker, []byte("x"), 0o600))
	t.Setenv("XDG_CACHE_HOME", blocker)

	is.True(SaveCache(Cache{}) != nil)

	Store(4, "A", 40, 100)

	_, ok := Lookup(4, "A")
	is.True(!ok)
}

func TestForgetUnwritableCacheDirDoesNotPanic(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	is := is.New(t)
	is.NoErr(os.WriteFile(blocker, []byte("x"), 0o600))
	t.Setenv("XDG_CACHE_HOME", blocker)

	Forget(4)
}

func TestCacheFileFormat(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	Store(4, "A", 40, 100)

	path, err := cache.Path(cacheName)
	is.NoErr(err)

	content, err := os.ReadFile(path)
	is.NoErr(err)

	// The file outlives the binary, so its field names are a compatibility contract.
	var raw map[string]map[string]any
	is.NoErr(json.Unmarshal(content, &raw))

	entry := raw["4"]
	is.Equal(entry["identity"], "A")
	is.Equal(entry["current"], float64(40))
	is.Equal(entry["maximum"], float64(100))
	_, ok := entry["updated_at"]
	is.True(ok)
}

func writeCacheFile(t *testing.T, content []byte) {
	t.Helper()

	path, err := cache.Path(cacheName)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}
