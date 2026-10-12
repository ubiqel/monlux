package monitor

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ubiqel/monlux/internal/cache"
)

const monitorsCacheName = "monitors.json"

// monitorsCacheTTL is how long a scan result is reused before scanning again.
const monitorsCacheTTL = 60 * time.Second

type monitorsCache struct {
	Monitors  []Monitor `json:"monitors"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Monitors returns the monitors on the host. It reuses a recent scan from the
// cache and scans again otherwise. Scans that find nothing aren't cached, so
// a monitor that is plugged in later is still found.
func (h Host) Monitors() []Monitor {
	cached, err := readMonitorsCache()
	switch {
	case err == nil:
		if age := time.Since(cached.UpdatedAt); age >= 0 && age <= monitorsCacheTTL {
			return cached.Monitors
		}
	case errors.Is(err, cache.ErrNotExist):
	default:
		slog.Warn("Failed to read monitors cache, scanning instead.", "error", err)
	}

	monitors := h.Scan().Monitors
	if len(monitors) == 0 {
		return monitors
	}

	if err := writeMonitorsCache(monitors); err != nil {
		slog.Warn("Failed to save monitors cache.", "error", err)
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
