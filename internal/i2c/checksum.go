package i2c

// Seeds are the implied address byte at the other end of the frame: 0x6E (the
// monitor's write address) for host-to-monitor, 0x50 (the host) for
// monitor-to-host.
const (
	seedHostToMonitor = byte(0x6E)
	seedMonitorToHost = byte(0x50)
)

func checksum(seed byte, data []byte) byte {
	sum := seed
	for _, b := range data {
		sum ^= b
	}

	return sum
}
