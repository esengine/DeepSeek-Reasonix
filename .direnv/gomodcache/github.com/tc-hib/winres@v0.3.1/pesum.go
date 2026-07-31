package winres

type peCheckSum struct {
	size uint32
	sum  uint32
	rem  uint32
}

// Sum returns the PE checksum of the PE file written with Write
func (c *peCheckSum) Sum() uint32 {
	if c.size&1 != 0 {
		return add16c(c.sum, c.rem) + c.size
	}
	return c.sum + c.size
}

// Write implements the Writer interface to compute the
// PE checksum of a PE file.
//
// That file must have its CheckSum field set to zero.
func (c *peCheckSum) Write(p []byte) (int, error) {
	l := uint32(len(p))
	o := c.size & 1
	m := (l-o)&^1 + o
	if o != 0 && l != 0 {
		c.sum = add16c(c.sum, uint32(p[0])<<8|c.rem)
	}
	for i := o; i < m; i += 2 {
		c.sum = add16c(c.sum, uint32(p[i+1])<<8|uint32(p[i]))
	}
	if m < l {
		c.rem = uint32(p[m])
	}
	c.size += l
	return len(p), nil
}

func add16c(a uint32, b uint32) uint32 {
	a += b
	return (a + (a >> 16)) & 0xFFFF
}
