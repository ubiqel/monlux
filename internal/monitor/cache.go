package monitor

import (
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/ubiqel/monlux/internal/cache"
)

const monitorsCacheName = "monitors.json"

const (
	monitorsCacheTTL = 60 * time.Second

	// A scan that misses a connected monitor is probably racing the monitor
	// waking up, so it gets a short TTL. If the same connector stays missing,
	// scanning every few seconds forever is wasteful, so the TTL settles.
	monitorsCachePartialTTL         = 5 * time.Second
	monitorsCachePartialSettledTTL  = 30 * time.Second
	monitorsCachePartialSettleAfter = 30 * time.Second
)

type monitorsCache struct {
	Monitors             []Monitor `json:"monitors"`
	ConnectorFingerprint string    `json:"connector_fingerprint"`
	Missing              []string  `json:"missing,omitempty"`
	PartialSince         time.Time `json:"partial_since,omitzero"`
	UpdatedAt            time.Time `json:"updated_at"`
}

func (c monitorsCache) ttl(now time.Time) time.Duration {
	if len(c.Missing) == 0 {
		return monitorsCacheTTL
	}

	if c.PartialSince.IsZero() || now.Sub(c.PartialSince) < monitorsCachePartialSettleAfter {
		return monitorsCachePartialTTL
	}

	return monitorsCachePartialSettledTTL
}

// Monitors returns the host's monitors, from the cache when it is fresh and
// the DRM connectors still match the cached state. Scans that find nothing
// aren't cached, so a monitor plugged in later is still found.
func (h Host) Monitors() []Monitor {
	if h.NoCache {
		slog.Debug("Monitors cache disabled, scanning.")

		return h.Scan().monitors
	}

	states, stateErr := h.connectorStates()
	if stateErr != nil {
		slog.Debug("Failed to read DRM connector state, skipping cache validation.", "error", stateErr)
	}

	fingerprint := ""
	if stateErr == nil {
		fingerprint = connectorFingerprint(states)
	}

	cached, err := readMonitorsCache()
	switch {
	case err == nil:
		now := time.Now()
		age := now.Sub(cached.UpdatedAt)
		ttl := cached.ttl(now)
		switch {
		case age < 0:
			slog.Debug("Monitors cache timestamp is in the future, scanning.", "age", age)
		case age > ttl:
			slog.Debug("Monitors cache expired, scanning.", "age", age, "ttl", ttl, "missing", cached.Missing)
		case stateErr != nil:
			slog.Debug("Could not read DRM connector state, scanning instead.")
		case cached.ConnectorFingerprint != fingerprint:
			slog.Debug("DRM connector state changed since the cache was written, scanning.",
				"cached", cached.ConnectorFingerprint, "current", fingerprint)
		default:
			slog.Debug("Monitors cache hit.", "age", age, "monitors", len(cached.Monitors), "missing", cached.Missing)

			return cached.Monitors
		}
	case errors.Is(err, cache.ErrNotExist):
		slog.Debug("Monitors cache miss, no cache file, scanning.")
	default:
		slog.Warn("Failed to read monitors cache, scanning instead.", "error", err)
	}

	res := h.scan(h.enrichConnectors(states))
	if len(res.monitors) == 0 {
		slog.Debug("Scan found no monitors, not caching the result.")

		return res.monitors
	}

	var partialSince time.Time
	if len(res.missing) > 0 {
		partialSince = partialStartTime(cached, err, fingerprint, res.missing, time.Now())
	}

	if err := writeMonitorsCache(res.monitors, fingerprint, res.missing, partialSince); err != nil {
		slog.Warn("Failed to save monitors cache.", "error", err)
	} else {
		slog.Debug("Saved monitors cache.",
			"monitors", len(res.monitors), "missing", res.missing, "partial_since", partialSince)
	}

	return res.monitors
}

// partialStartTime keeps the start of a persistent incomplete scan, so its TTL
// can settle, but restarts it when the cache or the missing connectors change.
func partialStartTime(cached monitorsCache, cacheErr error, fingerprint string, missing []string, now time.Time) time.Time {
	if cacheErr != nil ||
		cached.ConnectorFingerprint != fingerprint ||
		!slices.Equal(cached.Missing, missing) ||
		cached.PartialSince.IsZero() {
		return now
	}

	return cached.PartialSince
}

func readMonitorsCache() (monitorsCache, error) {
	var c monitorsCache
	if err := cache.ReadJSON(monitorsCacheName, &c); err != nil {
		return monitorsCache{}, fmt.Errorf("reading monitors cache: %w", err)
	}

	return c, nil
}

func writeMonitorsCache(monitors []Monitor, fingerprint string, missing []string, partialSince time.Time) error {
	err := cache.WriteJSON(monitorsCacheName, monitorsCache{
		Monitors:             monitors,
		ConnectorFingerprint: fingerprint,
		Missing:              missing,
		PartialSince:         partialSince,
		UpdatedAt:            time.Now(),
	})
	if err != nil {
		return fmt.Errorf("writing monitors cache: %w", err)
	}

	return nil
}
