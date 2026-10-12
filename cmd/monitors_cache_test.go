package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/matryer/is"
)

func isolateCache(t *testing.T) {
	t.Helper()

	t.Setenv("XDG_CACHE_HOME", t.TempDir())
}

// writeCacheFile writes a cache file with the given contents, bypassing
// writeMonitorsCache so the UpdatedAt timestamp can be chosen.
func writeCacheFile(t *testing.T, cache MonitorsCache) {
	t.Helper()

	is := is.New(t)

	path, err := resolveMonitorsCacheLocation()
	is.NoErr(err)

	content, err := json.Marshal(cache)
	is.NoErr(err)

	is.NoErr(os.WriteFile(path, content, 0o644))
}

func cacheFileExists(t *testing.T) bool {
	t.Helper()

	is := is.New(t)

	path, err := resolveMonitorsCacheLocation()
	is.NoErr(err)

	_, err = os.Stat(path)

	return err == nil
}

func TestWriteThenReadMonitorsCache(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	connector := "card0-DP-2"
	monitors := []Monitor{
		{I2CBus: 4, EDIDIdentity: "GBT1234", EDIDRaw: edidWith(7), DRMConnector: &connector},
		{I2CBus: 5, EDIDIdentity: "GBT5678", EDIDRaw: edidWith(9)},
	}

	before := time.Now()
	is.NoErr(writeMonitorsCache(monitors))

	got, err := readMonitorsCache()
	is.NoErr(err)

	is.True(!got.UpdatedAt.Before(before.Truncate(time.Second)))
	is.Equal(len(got.Monitors), 2)

	first := got.Monitors[0]
	is.Equal(first.I2CBus, 4)
	is.Equal(first.EDIDIdentity, "GBT1234")
	is.Equal(first.EDIDRaw, monitors[0].EDIDRaw)
	is.True(first.DRMConnector != nil && *first.DRMConnector == connector)

	is.True(got.Monitors[1].DRMConnector == nil)
}

func TestReadMonitorsCacheMissing(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	_, err := readMonitorsCache()
	is.True(errors.Is(err, ErrNoCache))
}

func TestReadMonitorsCacheCorruptIsRemoved(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	path, err := resolveMonitorsCacheLocation()
	is.NoErr(err)
	mustWrite(t, path, []byte("{not json"))

	_, err = readMonitorsCache()
	is.True(errors.Is(err, ErrNoCache))
	is.True(!cacheFileExists(t))
}

func TestReadMonitorsCacheUnreadableReturnsError(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	// A directory where the file should be can't be read as a file, and this
	// works even when the tests run as root.
	path, err := resolveMonitorsCacheLocation()
	is.NoErr(err)
	mustMkdir(t, path)

	_, err = readMonitorsCache()
	is.True(err != nil && !errors.Is(err, ErrNoCache))
}

func TestCacheDirIsTraversableByOwner(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	is.NoErr(writeMonitorsCache([]Monitor{{I2CBus: 4}}))

	path, err := resolveMonitorsCacheLocation()
	is.NoErr(err)

	info, err := os.Stat(filepath.Dir(path))
	is.NoErr(err)

	is.True(info.Mode().Perm()&0o700 == 0o700)
}

func TestScanMonitorsUsesFreshCache(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	reads := installFixture(t, fixture{
		gpus: []fxGPU{{pci: "0000:03:00.0", driver: "amdgpu", card: "card0", buses: []int{4}}},
		connectors: []fxConnector{
			{name: "card0-DP-2", gpu: 0, status: "connected", edid: makeEDID("ALPHA"), ddcBus: 4},
		},
		buses: map[int]fxBus{4: {edid: makeEDID("ALPHA"), ddc: true}},
	})

	// Bus 9 is not in the fixture, so getting it back proves the cache was used.
	writeCacheFile(t, MonitorsCache{
		Monitors:  []Monitor{{I2CBus: 9, EDIDIdentity: "CACHED"}},
		UpdatedAt: time.Now(),
	})

	got := scanMonitors()

	is.Equal(monitorBuses(ScanResult{Monitors: got}), []int{9})
	is.Equal(len(*reads), 0)
}

func TestScanMonitorsRescansStaleCache(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	installFixture(t, fixture{
		gpus: []fxGPU{{pci: "0000:03:00.0", driver: "amdgpu", card: "card0", buses: []int{4}}},
		connectors: []fxConnector{
			{name: "card0-DP-2", gpu: 0, status: "connected", edid: makeEDID("ALPHA"), ddcBus: 4},
		},
		buses: map[int]fxBus{4: {edid: makeEDID("ALPHA"), ddc: true}},
	})

	writeCacheFile(t, MonitorsCache{
		Monitors:  []Monitor{{I2CBus: 9, EDIDIdentity: "CACHED"}},
		UpdatedAt: time.Now().Add(-2 * cacheTTL),
	})

	got := scanMonitors()
	is.Equal(monitorBuses(ScanResult{Monitors: got}), []int{4})

	cached, err := readMonitorsCache()
	is.NoErr(err)
	is.Equal(monitorBuses(ScanResult{Monitors: cached.Monitors}), []int{4})
}

func TestScanMonitorsDoesNotCacheEmptyResult(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	installFixture(t, fixture{
		gpus: []fxGPU{{pci: "0000:03:00.0", driver: "amdgpu", card: "card0", buses: []int{4}}},
		connectors: []fxConnector{
			{name: "card0-DP-2", gpu: 0, status: "connected", edid: makeEDID("ALPHA"), ddcBus: 4},
		},
		buses: map[int]fxBus{4: {edid: makeEDID("ALPHA"), ddc: false}},
	})

	is.Equal(len(scanMonitors()), 0)
	is.True(!cacheFileExists(t))
}

func TestScanMonitorsSurvivesCacheWriteFailure(t *testing.T) {
	// A regular file where the cache directory should go makes every cache
	// operation fail. The scan must still return its monitors.
	blocker := filepath.Join(t.TempDir(), "blocker")
	mustWrite(t, blocker, []byte("x"))
	t.Setenv("XDG_CACHE_HOME", blocker)

	is := is.New(t)

	installFixture(t, fixture{
		gpus: []fxGPU{{pci: "0000:03:00.0", driver: "amdgpu", card: "card0", buses: []int{4}}},
		connectors: []fxConnector{
			{name: "card0-DP-2", gpu: 0, status: "connected", edid: makeEDID("ALPHA"), ddcBus: 4},
		},
		buses: map[int]fxBus{4: {edid: makeEDID("ALPHA"), ddc: true}},
	})

	got := scanMonitors()
	is.Equal(monitorBuses(ScanResult{Monitors: got}), []int{4})
}
