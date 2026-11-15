package monitor

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/matryer/is"
	"github.com/ubiqel/monlux/internal/cache"
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

func fingerprintFor(t *testing.T, host Host) string {
	t.Helper()

	states, err := host.connectorStates()
	if err != nil {
		t.Fatal(err)
	}

	return connectorFingerprint(states)
}

// partialFixture has one monitor answering DDC/CI and one connected monitor
// whose bus answers EDID but not DDC/CI.
func partialFixture(t *testing.T) (Host, *[]int, map[int]fxBus) {
	t.Helper()

	buses := map[int]fxBus{
		4: {edid: makeEDID("ALPHA"), ddc: true},
		5: {edid: makeEDID("BETA"), ddc: false},
	}

	host, reads := installFixture(t, fixture{
		gpus: []fxGPU{{pci: "0000:03:00.0", driver: "amdgpu", card: "card0", buses: []int{4, 5}}},
		connectors: []fxConnector{
			{name: "card0-DP-2", gpu: 0, status: "connected", edid: makeEDID("ALPHA"), ddcBus: 4},
			{name: "card0-DP-3", gpu: 0, status: "connected", edid: makeEDID("BETA"), ddcBus: 5},
		},
		buses: buses,
	})

	return host, reads, buses
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
	is.NoErr(writeMonitorsCache(monitors, "fingerprint", nil, time.Time{}))

	got, err := readMonitorsCache()
	is.NoErr(err)

	is.True(!got.UpdatedAt.Before(before.Truncate(time.Second)))
	is.Equal(got.ConnectorFingerprint, "fingerprint")
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

	is.NoErr(writeMonitorsCache([]Monitor{{I2CBus: 4}}, "", nil, time.Time{}))

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
		Monitors:             []Monitor{{I2CBus: 9, EDIDIdentity: "CACHED"}},
		ConnectorFingerprint: fingerprintFor(t, host),
		UpdatedAt:            time.Now(),
	})

	got := host.Monitors()

	is.Equal(monitorBuses(ScanResult{monitors: got}), []int{9})
	is.Equal(len(*reads), 0)
}

func TestMonitorsRescansWhenConnectorStateChanges(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	host, reads := installFixture(t, fixture{
		gpus: []fxGPU{{pci: "0000:03:00.0", driver: "amdgpu", card: "card0", buses: []int{4}}},
		connectors: []fxConnector{
			{name: "card0-DP-2", gpu: 0, status: "connected", edid: makeEDID("ALPHA"), ddcBus: 4},
		},
		buses: map[int]fxBus{4: {edid: makeEDID("ALPHA"), ddc: true}},
	})

	writeCacheFile(t, monitorsCache{
		Monitors:             []Monitor{{I2CBus: 9, EDIDIdentity: "CACHED"}},
		ConnectorFingerprint: fingerprintFor(t, host),
		UpdatedAt:            time.Now(),
	})

	// A monitor that wakes up after the cache was written changes its
	// connector's EDID, which must invalidate the cached list.
	mustWrite(t, filepath.Join(host.DRMPath, "card0-DP-2", "edid"), makeEDID("BETA"))

	got := host.Monitors()
	is.Equal(monitorBuses(ScanResult{monitors: got}), []int{4})
	is.Equal(len(*reads), 1)

	cached, err := readMonitorsCache()
	is.NoErr(err)
	is.Equal(cached.ConnectorFingerprint, fingerprintFor(t, host))
}

func TestMonitorsRescansCacheWithoutFingerprint(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	host, reads := installFixture(t, fixture{
		gpus: []fxGPU{{pci: "0000:03:00.0", driver: "amdgpu", card: "card0", buses: []int{4}}},
		connectors: []fxConnector{
			{name: "card0-DP-2", gpu: 0, status: "connected", edid: makeEDID("ALPHA"), ddcBus: 4},
		},
		buses: map[int]fxBus{4: {edid: makeEDID("ALPHA"), ddc: true}},
	})

	// A cache from an older monlux has no fingerprint and can't be trusted.
	writeCacheFile(t, monitorsCache{
		Monitors:  []Monitor{{I2CBus: 9, EDIDIdentity: "CACHED"}},
		UpdatedAt: time.Now(),
	})

	got := host.Monitors()
	is.Equal(monitorBuses(ScanResult{monitors: got}), []int{4})
	is.Equal(len(*reads), 1)
}

func TestConnectorFingerprint(t *testing.T) {
	is := is.New(t)

	base := []connectorState{
		{name: "card0-DP-1", status: "connected", edid: makeEDID("ALPHA")},
		{name: "card0-HDMI-A-1", status: "disconnected"},
	}

	fingerprint := connectorFingerprint(base)
	is.Equal(fingerprint, connectorFingerprint(base))

	changedStatus := slices.Clone(base)
	changedStatus[0].status = "disconnected"
	is.True(fingerprint != connectorFingerprint(changedStatus))

	changedEDID := slices.Clone(base)
	changedEDID[0].edid = makeEDID("BETA")
	is.True(fingerprint != connectorFingerprint(changedEDID))

	added := append(slices.Clone(base), connectorState{name: "card0-DP-2", status: "connected", edid: makeEDID("GAMMA")})
	is.True(fingerprint != connectorFingerprint(added))
}

func TestMonitorsCacheTTL(t *testing.T) {
	is := is.New(t)
	now := time.Now()

	complete := monitorsCache{}
	partial := monitorsCache{Missing: []string{"card0-DP-2"}, PartialSince: now.Add(-10 * time.Second)}
	settled := monitorsCache{Missing: []string{"card0-DP-2"}, PartialSince: now.Add(-monitorsCachePartialSettleAfter)}

	is.Equal(complete.ttl(now), monitorsCacheTTL)
	is.Equal(partial.ttl(now), monitorsCachePartialTTL)
	is.Equal(settled.ttl(now), monitorsCachePartialSettledTTL)
}

func TestMonitorsWritesPartialState(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	host, _, _ := partialFixture(t)

	got := host.Monitors()
	is.Equal(monitorBuses(ScanResult{monitors: got}), []int{4})

	cached, err := readMonitorsCache()
	is.NoErr(err)
	is.Equal(cached.Missing, []string{"card0-DP-3"})
	is.True(!cached.PartialSince.IsZero())
}

func TestMonitorsPartialCacheTTL(t *testing.T) {
	tests := []struct {
		name            string
		updatedAgo      time.Duration
		partialSinceAgo time.Duration
		wantCache       bool
	}{
		{"fresh partial", 4 * time.Second, 10 * time.Second, true},
		{"expired partial", 6 * time.Second, 10 * time.Second, false},
		{"settled partial inside ttl", 10 * time.Second, 40 * time.Second, true},
		{"settled partial expired", 31 * time.Second, 40 * time.Second, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateCache(t)
			is := is.New(t)

			host, reads, _ := partialFixture(t)

			writeCacheFile(t, monitorsCache{
				Monitors:             []Monitor{{I2CBus: 9, EDIDIdentity: "CACHED"}},
				ConnectorFingerprint: fingerprintFor(t, host),
				Missing:              []string{"card0-DP-3"},
				PartialSince:         time.Now().Add(-tt.partialSinceAgo),
				UpdatedAt:            time.Now().Add(-tt.updatedAgo),
			})

			got := host.Monitors()

			if tt.wantCache {
				is.Equal(monitorBuses(ScanResult{monitors: got}), []int{9})
				is.Equal(len(*reads), 0)
			} else {
				is.Equal(monitorBuses(ScanResult{monitors: got}), []int{4})
				is.True(len(*reads) > 0)
			}
		})
	}
}

func TestMonitorsCarriesPartialSince(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	host, reads, _ := partialFixture(t)
	since := time.Now().Add(-10 * time.Second)

	writeCacheFile(t, monitorsCache{
		Monitors:             []Monitor{{I2CBus: 9, EDIDIdentity: "CACHED"}},
		ConnectorFingerprint: fingerprintFor(t, host),
		Missing:              []string{"card0-DP-3"},
		PartialSince:         since,
		UpdatedAt:            time.Now().Add(-6 * time.Second),
	})

	got := host.Monitors()
	is.Equal(monitorBuses(ScanResult{monitors: got}), []int{4})
	is.True(len(*reads) > 0)

	cached, err := readMonitorsCache()
	is.NoErr(err)
	is.Equal(cached.PartialSince.UnixNano(), since.UnixNano())
}

func TestMonitorsResetsPartialSinceWhenMissingChanges(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	host, _, _ := partialFixture(t)
	since := time.Now().Add(-40 * time.Second)

	writeCacheFile(t, monitorsCache{
		Monitors:             []Monitor{{I2CBus: 9, EDIDIdentity: "CACHED"}},
		ConnectorFingerprint: fingerprintFor(t, host),
		Missing:              []string{"card0-DP-9"},
		PartialSince:         since,
		UpdatedAt:            time.Now().Add(-monitorsCachePartialSettledTTL),
	})

	host.Monitors()

	cached, err := readMonitorsCache()
	is.NoErr(err)
	is.Equal(cached.Missing, []string{"card0-DP-3"})
	is.True(cached.PartialSince.After(since))
}

func TestMonitorsCompleteScanClearsPartialState(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	host, _, buses := partialFixture(t)

	writeCacheFile(t, monitorsCache{
		Monitors:             []Monitor{{I2CBus: 9, EDIDIdentity: "CACHED"}},
		ConnectorFingerprint: fingerprintFor(t, host),
		Missing:              []string{"card0-DP-3"},
		PartialSince:         time.Now().Add(-10 * time.Second),
		UpdatedAt:            time.Now().Add(-6 * time.Second),
	})

	buses[5] = fxBus{edid: makeEDID("BETA"), ddc: true}

	got := host.Monitors()
	is.Equal(monitorBuses(ScanResult{monitors: got}), []int{4, 5})

	cached, err := readMonitorsCache()
	is.NoErr(err)
	is.Equal(len(cached.Missing), 0)
	is.True(cached.PartialSince.IsZero())
}

func TestMonitorsNoCacheIgnoresFreshCache(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	host, reads := installFixture(t, fixture{
		gpus: []fxGPU{{pci: "0000:03:00.0", driver: "amdgpu", card: "card0", buses: []int{4}}},
		connectors: []fxConnector{
			{name: "card0-DP-2", gpu: 0, status: "connected", edid: makeEDID("ALPHA"), ddcBus: 4},
		},
		buses: map[int]fxBus{4: {edid: makeEDID("ALPHA"), ddc: true}},
	})
	host.NoCache = true

	writeCacheFile(t, monitorsCache{
		Monitors:  []Monitor{{I2CBus: 9, EDIDIdentity: "CACHED"}},
		UpdatedAt: time.Now(),
	})

	got := host.Monitors()
	is.Equal(monitorBuses(ScanResult{monitors: got}), []int{4})
	is.Equal(len(*reads), 1)

	cached, err := readMonitorsCache()
	is.NoErr(err)
	is.Equal(monitorBuses(ScanResult{monitors: cached.Monitors}), []int{9})
}

func TestMonitorsNoCacheDoesNotWrite(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	host, _ := installFixture(t, fixture{
		gpus: []fxGPU{{pci: "0000:03:00.0", driver: "amdgpu", card: "card0", buses: []int{4}}},
		connectors: []fxConnector{
			{name: "card0-DP-2", gpu: 0, status: "connected", edid: makeEDID("ALPHA"), ddcBus: 4},
		},
		buses: map[int]fxBus{4: {edid: makeEDID("ALPHA"), ddc: true}},
	})
	host.NoCache = true

	is.Equal(monitorBuses(ScanResult{monitors: host.Monitors()}), []int{4})
	is.True(!cacheFileExists(t))
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
	is.Equal(monitorBuses(ScanResult{monitors: got}), []int{4})

	cached, err := readMonitorsCache()
	is.NoErr(err)
	is.Equal(monitorBuses(ScanResult{monitors: cached.Monitors}), []int{4})
}

func TestMonitorsRescansWhenCacheIsFromFuture(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	host, _ := installFixture(t, fixture{
		gpus: []fxGPU{{pci: "0000:03:00.0", driver: "amdgpu", card: "card0", buses: []int{4}}},
		connectors: []fxConnector{
			{name: "card0-DP-2", gpu: 0, status: "connected", edid: makeEDID("ALPHA"), ddcBus: 4},
		},
		buses: map[int]fxBus{4: {edid: makeEDID("ALPHA"), ddc: true}},
	})

	// A timestamp ahead of the clock (for example after a clock change) can't
	// be trusted, so the cache is ignored.
	writeCacheFile(t, monitorsCache{
		Monitors:  []Monitor{{I2CBus: 9, EDIDIdentity: "CACHED"}},
		UpdatedAt: time.Now().Add(time.Hour),
	})

	got := host.Monitors()
	is.Equal(monitorBuses(ScanResult{monitors: got}), []int{4})
}

func TestMonitorsScansWhenCacheIsUnreadable(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	host, reads := installFixture(t, fixture{
		gpus: []fxGPU{{pci: "0000:03:00.0", driver: "amdgpu", card: "card0", buses: []int{4}}},
		connectors: []fxConnector{
			{name: "card0-DP-2", gpu: 0, status: "connected", edid: makeEDID("ALPHA"), ddcBus: 4},
		},
		buses: map[int]fxBus{4: {edid: makeEDID("ALPHA"), ddc: true}},
	})

	// A directory where the cache file should be fails the read with an error
	// other than "not exist", which must not stop the scan.
	mustMkdir(t, monitorsCachePath(t))

	got := host.Monitors()
	is.Equal(monitorBuses(ScanResult{monitors: got}), []int{4})
	is.Equal(len(*reads), 1)
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
	is.Equal(monitorBuses(ScanResult{monitors: got}), []int{4})
}
