package brightness

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ubiqel/monlux/internal/cache"
)

const cacheName = "brightness.json"

const ttl = 5 * time.Second

type entry struct {
	Identity  string    `json:"identity"`
	Current   uint16    `json:"current"`
	Maximum   uint16    `json:"maximum"`
	UpdatedAt time.Time `json:"updated_at"`
}

type fileCache map[int]entry

func lookup(bus int, identity string) (entry, bool) {
	e, ok := loadCache()[bus]
	switch {
	case !ok:
		slog.Debug("Brightness cache miss, no entry for bus.", "bus", bus)

		return entry{}, false
	case e.Identity != identity:
		slog.Debug("Brightness cache miss, entry belongs to another monitor.", "bus", bus, "edid", identity, "cached_edid", e.Identity)

		return entry{}, false
	case e.Maximum == 0:
		slog.Debug("Brightness cache miss, entry has no maximum.", "bus", bus)

		return entry{}, false
	}

	return e, true
}

func store(bus int, identity string, current, maximum uint16) {
	c := loadCache()

	e := c[bus]
	if e.Identity != identity {
		// The bus now holds a different monitor, so its history doesn't apply.
		e = entry{Identity: identity}
	}
	e.Current = current
	e.Maximum = maximum
	e.UpdatedAt = time.Now()
	c[bus] = e

	if err := saveCache(c); err != nil {
		slog.Warn("Failed to save brightness cache.", "error", err)

		return
	}

	slog.Debug("Saved brightness cache.", "bus", bus, "raw", current, "maximum", maximum)
}

func forget(bus int) {
	c := loadCache()
	delete(c, bus)

	if err := saveCache(c); err != nil {
		slog.Warn("Failed to save brightness cache.", "error", err)

		return
	}

	slog.Debug("Removed brightness cache entry after failed write.", "bus", bus)
}

// loadCache reads the cache. Any failure yields an empty cache, since the
// cache is only an optimization.
func loadCache() fileCache {
	var c fileCache
	if err := cache.ReadJSON(cacheName, &c); err != nil {
		if !errors.Is(err, cache.ErrNotExist) {
			slog.Warn("Failed to read brightness cache.", "error", err)
		}

		return fileCache{}
	}
	if c == nil { // the file holds "null"
		c = fileCache{}
	}

	return c
}

// saveCache writes the cache. Commands that write hold the global lock, so
// writes don't race with each other.
func saveCache(c fileCache) error {
	if err := cache.WriteJSON(cacheName, c); err != nil {
		return fmt.Errorf("writing brightness cache: %w", err)
	}

	return nil
}

func (e entry) fresh(now time.Time) bool {
	age := now.Sub(e.UpdatedAt)

	return age >= 0 && age <= ttl
}
