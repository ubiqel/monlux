package brightness

import (
	"testing"
	"time"

	"github.com/matryer/is"
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
