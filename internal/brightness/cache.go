package brightness

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ubiqel/monlux/internal/cache"
)

const cacheName = "brightness.json"

const TTL = 5 * time.Second

type Entry struct {
	Identity  string    `json:"identity"`
	Current   uint16    `json:"current"`
	Maximum   uint16    `json:"maximum"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Cache map[int]Entry

func Lookup(bus int, identity string) (Entry, bool) {
	e, ok := LoadCache()[bus]
	switch {
	case !ok:
		slog.Debug("Brightness cache miss, no entry for bus.", "bus", bus)

		return Entry{}, false
	case e.Identity != identity:
		slog.Debug("Brightness cache miss, entry belongs to another monitor.", "bus", bus, "edid", identity, "cached_edid", e.Identity)

		return Entry{}, false
	case e.Maximum == 0:
		slog.Debug("Brightness cache miss, entry has no maximum.", "bus", bus)

		return Entry{}, false
	}

	return e, true
}

func Store(bus int, identity string, current, maximum uint16) {
	c := LoadCache()

	e := c[bus]
	if e.Identity != identity {
		// The bus now holds a different monitor, so its history doesn't apply.
		e = Entry{Identity: identity}
	}
	e.Current = current
	e.Maximum = maximum
	e.UpdatedAt = time.Now()
	c[bus] = e

	if err := SaveCache(c); err != nil {
		slog.Warn("Failed to save brightness cache.", "error", err)

		return
	}

	slog.Debug("Saved brightness cache.", "bus", bus, "raw", current, "maximum", maximum)
}

func Forget(bus int) {
	c := LoadCache()
	delete(c, bus)

	if err := SaveCache(c); err != nil {
		slog.Warn("Failed to save brightness cache.", "error", err)

		return
	}

	slog.Debug("Removed brightness cache entry after failed write.", "bus", bus)
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

func (e Entry) Fresh(now time.Time) bool {
	age := now.Sub(e.UpdatedAt)

	return age >= 0 && age <= TTL
}

func (e Entry) Percent() int {
	return int(e.Current) * 100 / int(e.Maximum)
}
