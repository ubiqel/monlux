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
	is := is.New(t)
	now := time.Now()

	tests := []struct {
		name    string
		updated time.Time
		want    bool
	}{
		{"just updated", now, true},
		{"within ttl", now.Add(-9 * time.Second), true},
		{"expired", now.Add(-11 * time.Second), false},
		{"updated in the future", now.Add(time.Second), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			is.Equal(Entry{UpdatedAt: tt.updated}.Fresh(now), tt.want)
		})
	}
}

func TestEntryDisagreesWithRecentWrite(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name    string
		written time.Time
		raw     uint16
		want    bool
	}{
		{"read differs right after write", now, 40, true},
		{"read matches the write", now, 50, false},
		{"read differs after the window", now.Add(-2 * time.Second), 40, false},
		{"never written", time.Time{}, 40, false},
		{"write in the future", now.Add(time.Second), 40, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			e := Entry{Current: 50, Maximum: 100, WrittenAt: tt.written}
			is.Equal(e.DisagreesWithRecentWrite(tt.raw, now), tt.want)
		})
	}
}

func TestStoreRoundTrip(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	Store(4, "A", 40, 100, true)
	Store(5, "B", 60, 200, false)

	is.Equal(len(LoadCache()), 2)

	a, ok := Lookup(4, "A")
	is.True(ok)
	is.Equal(a.Current, uint16(40))
	is.Equal(a.Maximum, uint16(100))
	is.True(!a.WrittenAt.IsZero())

	b, ok := Lookup(5, "B")
	is.True(ok)
	is.Equal(b.Current, uint16(60))
	is.True(b.WrittenAt.IsZero())

	// A later read keeps the write time, so the recent-write window still applies.
	Store(4, "A", 45, 100, false)
	a2, _ := Lookup(4, "A")
	is.Equal(a2.Current, uint16(45))
	is.True(a2.WrittenAt.Equal(a.WrittenAt))
}

func TestStoreReplugResetsEntry(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	Store(4, "OLD", 40, 100, true)
	Store(4, "NEW", 10, 100, false)

	e, ok := Lookup(4, "NEW")
	is.True(ok)
	is.Equal(e.Current, uint16(10))
	is.True(e.WrittenAt.IsZero())
}

func TestLookupRejectsOtherIdentity(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	Store(4, "A", 40, 100, false)

	_, ok := Lookup(4, "B")
	is.True(!ok)
}

func TestForgetRemovesEntry(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	Store(4, "A", 40, 100, false)
	Forget(4)

	_, ok := Lookup(4, "A")
	is.True(!ok)
}
