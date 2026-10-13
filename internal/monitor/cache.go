package monitor

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ubiqel/monlux/internal/cache"
)

const monitorsCacheName = "monitors.json"

const monitorsCacheTTL = 60 * time.Second

type monitorsCache struct {
	Monitors  []Monitor `json:"monitors"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Monitors returns the host's monitors, from the cache when it is fresh. Scans
// that find nothing aren't cached, so a monitor plugged in later is still found.
func (h Host) Monitors() []Monitor {
	if h.NoCache {
		slog.Debug("Monitors cache disabled, scanning.")

		return h.Scan().Monitors
	}

	cached, err := readMonitorsCache()
	switch {
	case err == nil:
		age := time.Since(cached.UpdatedAt)
		switch {
		case age < 0:
			slog.Debug("Monitors cache timestamp is in the future, scanning.", "age", age)
		case age > monitorsCacheTTL:
			slog.Debug("Monitors cache expired, scanning.", "age", age, "ttl", monitorsCacheTTL)
		default:
			slog.Debug("Monitors cache hit.", "age", age, "monitors", len(cached.Monitors))

			return cached.Monitors
		}
	case errors.Is(err, cache.ErrNotExist):
		slog.Debug("Monitors cache miss, no cache file, scanning.")
	default:
		slog.Warn("Failed to read monitors cache, scanning instead.", "error", err)
	}

	monitors := h.Scan().Monitors
	if len(monitors) == 0 {
		slog.Debug("Scan found no monitors, not caching the result.")

		return monitors
	}

	if err := writeMonitorsCache(monitors); err != nil {
		slog.Warn("Failed to save monitors cache.", "error", err)
	} else {
		slog.Debug("Saved monitors cache.", "monitors", len(monitors))
	}

	return monitors
}

func readMonitorsCache() (monitorsCache, error) {
	var c monitorsCache
	if err := cache.ReadJSON(monitorsCacheName, &c); err != nil {
		return monitorsCache{}, fmt.Errorf("reading monitors cache: %w", err)
	}

	return c, nil
}

func writeMonitorsCache(monitors []Monitor) error {
	err := cache.WriteJSON(monitorsCacheName, monitorsCache{
		Monitors:  monitors,
		UpdatedAt: time.Now(),
	})
	if err != nil {
		return fmt.Errorf("writing monitors cache: %w", err)
	}

	return nil
}
