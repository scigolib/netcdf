package cdf

import (
	"fmt"
	"io"
	"math"
)

// readChunk is the window the header reader keeps in memory. The header is
// parsed sequentially; names and attribute values are copied out in chunks, so
// no allocation grows beyond what the input actually holds.
const readChunk = 32 << 10

// cursor reads the header sequentially from r, never past size, and reports
// every short read as corruption.
type cursor struct {
	r    io.ReaderAt
	size int64
	pos  int64

	base int64 // file offset of buf[0]
	buf  [readChunk]byte
	n    int // valid bytes in buf
}

func newCursor(r io.ReaderAt, size int64) *cursor {
	return &cursor{r: r, size: size}
}

func (c *cursor) remaining() int64 { return c.size - c.pos }

func (c *cursor) corrupt(format string, args ...any) error {
	return corruptf("at offset %d: %s", c.pos, fmt.Sprintf(format, args...))
}

// window returns the next n bytes of the file without consuming them.
func (c *cursor) window(n int) ([]byte, error) {
	if c.pos >= c.base && c.pos+int64(n) <= c.base+int64(c.n) {
		i := int(c.pos - c.base)
		return c.buf[i : i+n], nil
	}
	if int64(n) > c.remaining() {
		return nil, c.corrupt("need %d bytes, %d remain", n, c.remaining())
	}
	want := int64(readChunk)
	if want > c.remaining() {
		want = c.remaining()
	}
	if _, err := c.r.ReadAt(c.buf[:want], c.pos); err != nil {
		return nil, c.corrupt("reading %d bytes: %v", want, err)
	}
	c.base = c.pos
	c.n = int(want)
	return c.buf[:n], nil
}

func (c *cursor) u32() (uint32, error) {
	b, err := c.window(4)
	if err != nil {
		return 0, err
	}
	c.pos += 4
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3]), nil
}

// u64 reads an unsigned 64-bit value.
func (c *cursor) u64() (uint64, error) {
	b, err := c.window(8)
	if err != nil {
		return 0, err
	}
	c.pos += 8
	return uint64(b[0])<<56 | uint64(b[1])<<48 | uint64(b[2])<<40 | uint64(b[3])<<32 |
		uint64(b[4])<<24 | uint64(b[5])<<16 | uint64(b[6])<<8 | uint64(b[7]), nil
}

// st reads a size_t-shaped integer: 32 bits, or 64 in CDF-5. A CDF-5 value
// above math.MaxInt64 cannot describe anything addressable and is rejected.
func (c *cursor) st(v Version) (int64, error) {
	if v == Version5 {
		u, err := c.u64()
		if err != nil {
			return 0, err
		}
		if u > 1<<63-1 {
			return 0, c.corrupt("64-bit size %d exceeds int64", u)
		}
		return int64(u), nil
	}
	u, err := c.u32()
	if err != nil {
		return 0, err
	}
	return int64(u), nil
}

// ofs reads a variable's beginning offset: 32 bits in CDF-1, 64 in CDF-2 and
// CDF-5. The specification defines OFFSET as a non-negative signed INT in
// CDF-1, so the top bit must be clear (netCDF-C sign-extends and rejects a
// non-positive begin); the 64-bit forms must fit int64.
func (c *cursor) ofs(v Version) (int64, error) {
	if v == Version1 {
		u, err := c.u32()
		if err != nil {
			return 0, err
		}
		if u > math.MaxInt32 {
			return 0, c.corrupt("begin offset %d exceeds the CDF-1 range", u)
		}
		return int64(u), nil
	}
	u, err := c.u64()
	if err != nil {
		return 0, err
	}
	if u > math.MaxInt64 {
		return 0, c.corrupt("offset %d exceeds int64", u)
	}
	return int64(u), nil
}

// skip advances over n bytes that need not be interpreted.
func (c *cursor) skip(n int64) error {
	if n < 0 || n > c.remaining() {
		return c.corrupt("skipping %d bytes, %d remain", n, c.remaining())
	}
	c.pos += n
	return nil
}

// bytes returns a copy of the next n bytes.
func (c *cursor) bytes(n int64) ([]byte, error) {
	if n < 0 || n > c.remaining() {
		return nil, c.corrupt("need %d bytes, %d remain", n, c.remaining())
	}
	if n > maxAlloc {
		return nil, c.corrupt("declared %d bytes exceeds the %d-byte limit", n, int64(maxAlloc))
	}
	b := make([]byte, n)
	done := int64(0)
	for done < n {
		chunk := n - done
		if chunk > readChunk {
			chunk = readChunk
		}
		w, err := c.window(int(chunk))
		if err != nil {
			return nil, err
		}
		copy(b[done:], w)
		c.pos += chunk
		done += chunk
	}
	return b, nil
}

// pad4 returns the number of padding bytes that follow n bytes.
func pad4(n int64) int64 { return (4 - n%4) % 4 }

// name reads a length-prefixed, 4-byte-padded name.
func (c *cursor) name(v Version) (string, error) {
	n, err := c.st(v)
	if err != nil {
		return "", err
	}
	if n < 0 {
		return "", c.corrupt("negative name length %d", n)
	}
	if n > maxNameLen {
		return "", c.corrupt("name length %d exceeds the %d-byte limit", n, int64(maxNameLen))
	}
	b, err := c.bytes(n)
	if err != nil {
		return "", err
	}
	if err := c.skip(pad4(n)); err != nil {
		return "", err
	}
	return string(b), nil
}
