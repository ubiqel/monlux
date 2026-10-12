package brightness

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ubiqel/shine/internal/cache"
)

const cacheName = "brightness.json"

// TTL is how long a cached brightness value can be used without reading the
// monitor again.
const TTL = 10 * time.Second

// recentWriteWindow is how long after a write a read that disagrees with it is
// ignored.
const recentWriteWindow = time.Second

// Entry is the last known brightness of the monitor on one I2C bus.
type Entry struct {
	Identity  string    `json:"identity"`
	Current   uint16    `json:"current"`
	Maximum   uint16    `json:"maximum"`
	UpdatedAt time.Time `json:"updated_at"` // last time the value was read or written
	WrittenAt time.Time `json:"written_at"` // last time shine wrote the value
}

// Cache holds the entries, keyed by I2C bus.
type Cache map[int]Entry

// Lookup returns the entry for the monitor with identity on bus. It returns
// false if the bus holds a different monitor or has no usable entry.
func Lookup(bus int, identity string) (Entry, bool) {
	e, ok := LoadCache()[bus]
	if !ok || e.Identity != identity || e.Maximum == 0 {
		return Entry{}, false
	}

	return e, true
}

// Store records a value read from (written=false) or written to (written=true)
// the monitor with identity on bus.
func Store(bus int, identity string, current, maximum uint16, written bool) {
	c := LoadCache()
	now := time.Now()

	e := c[bus]
	if e.Identity != identity {
		// The bus now holds a different monitor, so its history doesn't apply.
		e = Entry{Identity: identity}
	}
	e.Current = current
	e.Maximum = maximum
	e.UpdatedAt = now
	if written {
		e.WrittenAt = now
	}
	c[bus] = e

	if err := SaveCache(c); err != nil {
		slog.Warn("Failed to save brightness cache.", "error", err)
	}
}

// Forget removes the entry for bus.
func Forget(bus int) {
	c := LoadCache()
	delete(c, bus)

	if err := SaveCache(c); err != nil {
		slog.Warn("Failed to save brightness cache.", "error", err)
	}
}

// LoadCache reads the cache. Any failure yields an empty cache, since the
// cache is only an optimization.
func LoadCache() Cache {
	var c Cache
	if err := cache.ReadJSON(cacheName, &c); err != nil {
		if !errors.Is(err, cache.ErrNotExist) {
			slog.Warn("Failed to read brightness cache.", "error", err)
		}

		return Cache{}
	}
	if c == nil { // the file holds "null"
		c = Cache{}
	}

	return c
}

// SaveCache writes the cache. Commands that write hold the global lock, so
// writes don't race with each other.
func SaveCache(c Cache) error {
	if err := cache.WriteJSON(cacheName, c); err != nil {
		return fmt.Errorf("writing brightness cache: %w", err)
	}

	return nil
}

// Fresh reports whether the value was read or written recently enough to use
// without reading the monitor.
func (e Entry) Fresh(now time.Time) bool {
	age := now.Sub(e.UpdatedAt)

	return age >= 0 && age <= TTL
}

// DisagreesWithRecentWrite reports whether raw, read from the monitor, differs
// from a value written moments ago. Such a read probably happened before the
// monitor applied the write.
func (e Entry) DisagreesWithRecentWrite(raw uint16, now time.Time) bool {
	if e.WrittenAt.IsZero() {
		return false
	}

	age := now.Sub(e.WrittenAt)

	return age >= 0 && age <= recentWriteWindow && raw != e.Current
}

// Percent returns the cached value as a percentage of the maximum.
func (e Entry) Percent() int {
	return int(e.Current) * 100 / int(e.Maximum)
}
