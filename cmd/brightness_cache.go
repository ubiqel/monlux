package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/ubiqel/shine/internal/i2c"
	"github.com/urfave/cli/v3"
)

const noBrightnessCacheFlag = "no-brightness-cache"

const brightnessCacheName = "brightness.json"

const brightnessTTL = 10 * time.Second

const recentWriteWindow = time.Second

type brightnessEntry struct {
	Identity  string    `json:"identity"`
	Current   uint16    `json:"current"`
	Maximum   uint16    `json:"maximum"`
	UpdatedAt time.Time `json:"updated_at"` // last time the value was read or written
	WrittenAt time.Time `json:"written_at"` // last time shine wrote the value
}

type brightnessCache map[int]brightnessEntry

func noBrightnessCacheFlagDef() *cli.BoolFlag {
	return &cli.BoolFlag{
		Name:  noBrightnessCacheFlag,
		Usage: "read and write brightness on the monitor, ignoring the brightness cache",
	}
}

func currentBrightness(fd int, m Monitor, useCache bool) (pct int, maximum uint16, err error) {
	if useCache {
		slog.Debug("Trying to find brightness value in cache", "edid", m.EDIDIdentity)
		if e, ok := entryFor(loadBrightnessCache(), m); ok && usableAsBase(e, time.Now()) {
			return percentOf(e.Current, e.Maximum), e.Maximum, nil
		}
	}

	pct, raw, maximum, err := i2c.ReadBrightness(fd)
	if err != nil {
		return 0, 0, err
	}

	if useCache {
		storeBrightness(m, raw, maximum, false)
	}

	return pct, maximum, nil
}

func writeBrightness(fd int, m Monitor, raw, maximum uint16, useCache bool) error {
	if err := i2c.SetBrightness(fd, raw); err != nil {
		if useCache {
			forgetBrightness(m)
		}

		return err
	}

	if useCache {
		storeBrightness(m, raw, maximum, true)
	}

	return nil
}

// observeBrightness returns the percentage to report for a value just read
// from m, and records the value. A read that disagrees with a write made moments
// ago is ignored, since the monitor probably hasn't applied the write yet.
func observeBrightness(m Monitor, pct int, raw, maximum uint16) int {
	if e, ok := entryFor(loadBrightnessCache(), m); ok && readDisagreesWithRecentWrite(e, raw, time.Now()) {
		return percentOf(e.Current, e.Maximum)
	}

	storeBrightness(m, raw, maximum, false)

	return pct
}

func storeBrightness(m Monitor, current, maximum uint16, written bool) {
	c := loadBrightnessCache()
	now := time.Now()

	e := c[m.I2CBus]
	if e.Identity != m.EDIDIdentity {
		// The bus now holds a different monitor, so its history doesn't apply.
		e = brightnessEntry{Identity: m.EDIDIdentity}
	}
	e.Current = current
	e.Maximum = maximum
	e.UpdatedAt = now
	if written {
		e.WrittenAt = now
	}
	c[m.I2CBus] = e

	if err := saveBrightnessCache(c); err != nil {
		slog.Warn("Failed to save brightness cache.", "error", err)
	}
}

func forgetBrightness(m Monitor) {
	c := loadBrightnessCache()
	delete(c, m.I2CBus)

	if err := saveBrightnessCache(c); err != nil {
		slog.Warn("Failed to save brightness cache.", "error", err)
	}
}

// loadBrightnessCache reads the cache. Any failure yields an empty cache, since
// the cache is only an optimization.
func loadBrightnessCache() brightnessCache {
	path, err := resolveCacheFile(brightnessCacheName)
	if err != nil {
		slog.Warn("Couldn't resolve brightness cache location.", "error", err)

		return brightnessCache{}
	}

	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return brightnessCache{}
	}
	if err != nil {
		slog.Warn("Failed to read brightness cache.", "error", err)

		return brightnessCache{}
	}

	var c brightnessCache
	if err := json.Unmarshal(content, &c); err != nil {
		slog.Warn("Failed to unmarshal brightness cache, ignoring it.", "error", err)

		return brightnessCache{}
	}
	if c == nil {
		c = brightnessCache{}
	}

	return c
}

// saveBrightnessCache writes the cache. Commands that write hold the global
// lock, so writes don't race with each other.
func saveBrightnessCache(c brightnessCache) error {
	path, err := resolveCacheFile(brightnessCacheName)
	if err != nil {
		return err
	}

	content, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshalling to json: %w", err)
	}

	if err := os.WriteFile(path, content, 0o644); err != nil {
		return fmt.Errorf("writing brightness cache file: %w", err)
	}

	return nil
}

func entryFor(c brightnessCache, m Monitor) (brightnessEntry, bool) {
	e, ok := c[m.I2CBus]
	if !ok || e.Identity != m.EDIDIdentity || e.Maximum == 0 {
		return brightnessEntry{}, false
	}

	return e, true
}

func usableAsBase(e brightnessEntry, now time.Time) bool {
	age := now.Sub(e.UpdatedAt)

	return age >= 0 && age <= brightnessTTL
}

func readDisagreesWithRecentWrite(e brightnessEntry, raw uint16, now time.Time) bool {
	if e.WrittenAt.IsZero() {
		return false
	}

	age := now.Sub(e.WrittenAt)

	return age >= 0 && age <= recentWriteWindow && raw != e.Current
}

func percentOf(current, maximum uint16) int {
	return int(current) * 100 / int(maximum)
}
