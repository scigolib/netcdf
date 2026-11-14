package netcdf

import "fmt"

// readChunkElems caps how many values one read decodes at a time: 16384
// elements, at most 128 KiB of scratch space whatever the type.
const readChunkElems = 16384

// readRuns decodes the selection into dst through its codec. The caller has
// already validated the selection and matched dst's length to expect; the
// runs come from the format layer in logical order and never overlap.
func readRuns(v *Variable, c codec, dst any, expect int64, start, count []int64) error {
	elem := int64(c.size)
	scratch := make([]byte, readChunkElems*elem)
	base := int64(0)
	err := v.file.h.WalkVara(v.v, start, count, func(off, n int64) error {
		if off < 0 || off > v.file.size || n > (v.file.size-off)/elem {
			return fmt.Errorf("%w: variable %q: %d values at offset %d do not fit in the %d-byte file",
				ErrCorrupt, v.v.Name, n, off, v.file.size)
		}
		for done := int64(0); done < n; {
			k := n - done
			if k > readChunkElems {
				k = readChunkElems
			}
			buf := scratch[:k*elem]
			if _, err := v.file.r.ReadAt(buf, off+done*elem); err != nil {
				return fmt.Errorf("%w: variable %q: reading %d bytes at offset %d: %w",
					ErrCorrupt, v.v.Name, len(buf), off+done*elem, err)
			}
			if !c.decode(dst, base+done, buf) {
				return v.mismatch(dst)
			}
			done += k
		}
		base += n
		return nil
	})
	if err != nil {
		return translate(err)
	}
	if base != expect {
		return fmt.Errorf("%w: variable %q: the selection yielded %d of %d values",
			ErrCorrupt, v.v.Name, base, expect)
	}
	return nil
}
