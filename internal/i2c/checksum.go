package i2c

func checksumWrite(data []byte) byte { // data = on-wire bytes; dest 0x6E implied
	cs := byte(0x6E)
	for _, b := range data {
		cs ^= b
	}

	return cs
}

func checksumRead(data []byte) byte { // data = reply bytes excluding the final checksum
	cs := byte(0x50)
	for _, b := range data {
		cs ^= b
	}

	return cs
}
