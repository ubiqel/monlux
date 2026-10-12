package brightness

import (
	"testing"
	"time"

	"github.com/matryer/is"
	"github.com/ubiqel/shine/internal/monitor"
)

// noDevice is a fd that can't be used for DDC/CI, so any hardware access fails.
const noDevice = -1

func TestCurrentUsesFreshCache(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	is.NoErr(SaveCache(Cache{
		4: {Identity: "A", Current: 40, Maximum: 100, UpdatedAt: time.Now()},
	}))

	pct, maximum, err := Current(noDevice, monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}, true)
	is.NoErr(err)
	is.Equal(pct, 40)
	is.Equal(maximum, uint16(100))
}

func TestCurrentReadsWhenNotUsable(t *testing.T) {
	isolateCache(t)

	now := time.Now()
	fresh := Entry{Identity: "A", Current: 40, Maximum: 100, UpdatedAt: now}
	stale := fresh
	stale.UpdatedAt = now.Add(-2 * TTL)

	tests := []struct {
		name     string
		entry    Entry
		monitor  monitor.Monitor
		useCache bool
	}{
		{"stale entry", stale, monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}, true},
		{"entry for another monitor", fresh, monitor.Monitor{I2CBus: 4, EDIDIdentity: "B"}, true},
		{"cache disabled", fresh, monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			is.NoErr(SaveCache(Cache{4: tt.entry}))

			_, _, err := Current(noDevice, tt.monitor, tt.useCache)
			is.True(err != nil)
		})
	}
}

func TestWriteFailureForgetsEntry(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	m := monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}
	Store(m.I2CBus, m.EDIDIdentity, 40, 100, false)

	err := Write(noDevice, m, 50, 100, true)
	is.True(err != nil)

	_, ok := Lookup(m.I2CBus, m.EDIDIdentity)
	is.True(!ok) // a failed write leaves the monitor's state unknown
}

func TestWriteWithoutCacheLeavesCacheAlone(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	m := monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}
	Store(m.I2CBus, m.EDIDIdentity, 40, 100, false)

	is.True(Write(noDevice, m, 50, 100, false) != nil)

	e, ok := Lookup(m.I2CBus, m.EDIDIdentity)
	is.True(ok)
	is.Equal(e.Current, uint16(40))
}

func TestObserveIgnoresReadRightAfterWrite(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	m := monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}
	Store(m.I2CBus, m.EDIDIdentity, 50, 100, true)

	pct := Observe(m, 40, 40, 100)
	is.Equal(pct, 50)

	e, _ := Lookup(m.I2CBus, m.EDIDIdentity)
	is.Equal(e.Current, uint16(50))
}

func TestObserveRecordsRead(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	m := monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}

	pct := Observe(m, 40, 40, 100)
	is.Equal(pct, 40)

	e, ok := Lookup(m.I2CBus, m.EDIDIdentity)
	is.True(ok)
	is.Equal(e.Current, uint16(40))
}

func TestClamp(t *testing.T) {
	is := is.New(t)

	is.Equal(Clamp(-5, 0, 100), 0)
	is.Equal(Clamp(120, 0, 100), 100)
	is.Equal(Clamp(40, 0, 100), 40)
}
