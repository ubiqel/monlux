package main

import (
	"testing"
	"time"

	"github.com/matryer/is"
)

// noDevice is a fd that can't be used for DDC/CI, so any hardware access fails.
const noDevice = -1

func TestUsableAsBase(t *testing.T) {
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

			is.Equal(usableAsBase(brightnessEntry{UpdatedAt: tt.updated}, now), tt.want)
		})
	}
}

func TestReadDisagreesWithRecentWrite(t *testing.T) {
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

			e := brightnessEntry{Current: 50, Maximum: 100, WrittenAt: tt.written}
			is.Equal(readDisagreesWithRecentWrite(e, tt.raw, now), tt.want)
		})
	}
}

func TestStoreBrightnessRoundTrip(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	written := Monitor{I2CBus: 4, EDIDIdentity: "A"}
	read := Monitor{I2CBus: 5, EDIDIdentity: "B"}

	storeBrightness(written, 40, 100, true)
	storeBrightness(read, 60, 200, false)

	c := loadBrightnessCache()
	is.Equal(len(c), 2)

	a, ok := entryFor(c, written)
	is.True(ok)
	is.Equal(a.Current, uint16(40))
	is.Equal(a.Maximum, uint16(100))
	is.True(!a.WrittenAt.IsZero())

	b, ok := entryFor(c, read)
	is.True(ok)
	is.Equal(b.Current, uint16(60))
	is.True(b.WrittenAt.IsZero())

	// A later read keeps the write time, so the recent-write window still applies.
	storeBrightness(written, 45, 100, false)
	c = loadBrightnessCache()
	a2, _ := entryFor(c, written)
	is.Equal(a2.Current, uint16(45))
	is.True(a2.WrittenAt.Equal(a.WrittenAt))
}

func TestStoreBrightnessReplugResetsEntry(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	storeBrightness(Monitor{I2CBus: 4, EDIDIdentity: "OLD"}, 40, 100, true)
	storeBrightness(Monitor{I2CBus: 4, EDIDIdentity: "NEW"}, 10, 100, false)

	e, ok := entryFor(loadBrightnessCache(), Monitor{I2CBus: 4, EDIDIdentity: "NEW"})
	is.True(ok)
	is.Equal(e.Current, uint16(10))
	is.True(e.WrittenAt.IsZero())
}

func TestCurrentBrightnessUsesFreshCache(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	is.NoErr(saveBrightnessCache(brightnessCache{
		4: {Identity: "A", Current: 40, Maximum: 100, UpdatedAt: time.Now()},
	}))

	pct, maximum, err := currentBrightness(noDevice, Monitor{I2CBus: 4, EDIDIdentity: "A"}, true)
	is.NoErr(err)
	is.Equal(pct, 40)
	is.Equal(maximum, uint16(100))
}

func TestCurrentBrightnessReadsWhenNotUsable(t *testing.T) {
	isolateCache(t)

	now := time.Now()
	fresh := brightnessEntry{Identity: "A", Current: 40, Maximum: 100, UpdatedAt: now}
	stale := fresh
	stale.UpdatedAt = now.Add(-2 * brightnessTTL)

	tests := []struct {
		name     string
		entry    brightnessEntry
		monitor  Monitor
		useCache bool
	}{
		{"stale entry", stale, Monitor{I2CBus: 4, EDIDIdentity: "A"}, true},
		{"entry for another monitor", fresh, Monitor{I2CBus: 4, EDIDIdentity: "B"}, true},
		{"cache disabled", fresh, Monitor{I2CBus: 4, EDIDIdentity: "A"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			is.NoErr(saveBrightnessCache(brightnessCache{4: tt.entry}))

			_, _, err := currentBrightness(noDevice, tt.monitor, tt.useCache)
			is.True(err != nil)
		})
	}
}

func TestWriteBrightnessFailureForgetsEntry(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	m := Monitor{I2CBus: 4, EDIDIdentity: "A"}
	storeBrightness(m, 40, 100, false)

	err := writeBrightness(noDevice, m, 50, 100, true)
	is.True(err != nil)

	_, ok := entryFor(loadBrightnessCache(), m)
	is.True(!ok) // a failed write leaves the monitor's state unknown
}

func TestWriteBrightnessWithoutCacheLeavesCacheAlone(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	m := Monitor{I2CBus: 4, EDIDIdentity: "A"}
	storeBrightness(m, 40, 100, false)

	is.True(writeBrightness(noDevice, m, 50, 100, false) != nil)

	e, ok := entryFor(loadBrightnessCache(), m)
	is.True(ok)
	is.Equal(e.Current, uint16(40))
}

func TestObserveBrightnessIgnoresReadRightAfterWrite(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	m := Monitor{I2CBus: 4, EDIDIdentity: "A"}
	storeBrightness(m, 50, 100, true)

	pct := observeBrightness(m, 40, 40, 100)
	is.Equal(pct, 50)

	e, _ := entryFor(loadBrightnessCache(), m)
	is.Equal(e.Current, uint16(50))
}

func TestObserveBrightnessRecordsRead(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	m := Monitor{I2CBus: 4, EDIDIdentity: "A"}

	pct := observeBrightness(m, 40, 40, 100)
	is.Equal(pct, 40)

	e, ok := entryFor(loadBrightnessCache(), m)
	is.True(ok)
	is.Equal(e.Current, uint16(40))
}
