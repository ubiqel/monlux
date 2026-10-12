package monitor

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/matryer/is"
	"github.com/ubiqel/shine/internal/cache"
)

func isolateCache(t *testing.T) {
	t.Helper()

	t.Setenv("XDG_CACHE_HOME", t.TempDir())
}

func monitorsCachePath(t *testing.T) string {
	t.Helper()

	path, err := cache.Path(monitorsCacheName)
	if err != nil {
		t.Fatal(err)
	}

	return path
}

// writeCacheFile writes a cache file with the given contents, bypassing
// writeMonitorsCache so the UpdatedAt timestamp can be chosen.
func writeCacheFile(t *testing.T, c monitorsCache) {
	t.Helper()

	is := is.New(t)

	content, err := json.Marshal(c)
	is.NoErr(err)

	is.NoErr(os.WriteFile(monitorsCachePath(t), content, 0o644))
}

func cacheFileExists(t *testing.T) bool {
	t.Helper()

	_, err := os.Stat(monitorsCachePath(t))

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
	is.True(errors.Is(err, cache.ErrNotExist))
}

func TestReadMonitorsCacheCorruptIsRemoved(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	is.NoErr(os.WriteFile(monitorsCachePath(t), []byte("{not json"), 0o644))

	_, err := readMonitorsCache()
	is.True(errors.Is(err, cache.ErrNotExist))
	is.True(!cacheFileExists(t))
}

func TestReadMonitorsCacheUnreadableReturnsError(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	// A directory where the file should be can't be read as a file, and this
	// works even when the tests run as root.
	mustMkdir(t, monitorsCachePath(t))

	_, err := readMonitorsCache()
	is.True(err != nil && !errors.Is(err, cache.ErrNotExist))
}

func TestCacheDirIsTraversableByOwner(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	is.NoErr(writeMonitorsCache([]Monitor{{I2CBus: 4}}))

	info, err := os.Stat(filepath.Dir(monitorsCachePath(t)))
	is.NoErr(err)

	is.True(info.Mode().Perm()&0o700 == 0o700)
}

func TestMonitorsUsesFreshCache(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	host, reads := installFixture(t, fixture{
		gpus: []fxGPU{{pci: "0000:03:00.0", driver: "amdgpu", card: "card0", buses: []int{4}}},
		connectors: []fxConnector{
			{name: "card0-DP-2", gpu: 0, status: "connected", edid: makeEDID("ALPHA"), ddcBus: 4},
		},
		buses: map[int]fxBus{4: {edid: makeEDID("ALPHA"), ddc: true}},
	})

	// Bus 9 is not in the fixture, so getting it back proves the cache was used.
	writeCacheFile(t, monitorsCache{
		Monitors:  []Monitor{{I2CBus: 9, EDIDIdentity: "CACHED"}},
		UpdatedAt: time.Now(),
	})

	got := host.Monitors()

	is.Equal(monitorBuses(ScanResult{Monitors: got}), []int{9})
	is.Equal(len(*reads), 0)
}

func TestMonitorsRescansStaleCache(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	host, _ := installFixture(t, fixture{
		gpus: []fxGPU{{pci: "0000:03:00.0", driver: "amdgpu", card: "card0", buses: []int{4}}},
		connectors: []fxConnector{
			{name: "card0-DP-2", gpu: 0, status: "connected", edid: makeEDID("ALPHA"), ddcBus: 4},
		},
		buses: map[int]fxBus{4: {edid: makeEDID("ALPHA"), ddc: true}},
	})

	writeCacheFile(t, monitorsCache{
		Monitors:  []Monitor{{I2CBus: 9, EDIDIdentity: "CACHED"}},
		UpdatedAt: time.Now().Add(-2 * monitorsCacheTTL),
	})

	got := host.Monitors()
	is.Equal(monitorBuses(ScanResult{Monitors: got}), []int{4})

	cached, err := readMonitorsCache()
	is.NoErr(err)
	is.Equal(monitorBuses(ScanResult{Monitors: cached.Monitors}), []int{4})
}

func TestMonitorsDoesNotCacheEmptyResult(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	host, _ := installFixture(t, fixture{
		gpus: []fxGPU{{pci: "0000:03:00.0", driver: "amdgpu", card: "card0", buses: []int{4}}},
		connectors: []fxConnector{
			{name: "card0-DP-2", gpu: 0, status: "connected", edid: makeEDID("ALPHA"), ddcBus: 4},
		},
		buses: map[int]fxBus{4: {edid: makeEDID("ALPHA"), ddc: false}},
	})

	is.Equal(len(host.Monitors()), 0)
	is.True(!cacheFileExists(t))
}

func TestMonitorsSurvivesCacheWriteFailure(t *testing.T) {
	// A regular file where the cache directory should go makes every cache
	// operation fail. The scan must still return its monitors.
	blocker := filepath.Join(t.TempDir(), "blocker")
	mustWrite(t, blocker, []byte("x"))
	t.Setenv("XDG_CACHE_HOME", blocker)

	is := is.New(t)

	host, _ := installFixture(t, fixture{
		gpus: []fxGPU{{pci: "0000:03:00.0", driver: "amdgpu", card: "card0", buses: []int{4}}},
		connectors: []fxConnector{
			{name: "card0-DP-2", gpu: 0, status: "connected", edid: makeEDID("ALPHA"), ddcBus: 4},
		},
		buses: map[int]fxBus{4: {edid: makeEDID("ALPHA"), ddc: true}},
	})

	got := host.Monitors()
	is.Equal(monitorBuses(ScanResult{Monitors: got}), []int{4})
}
