package netcdf

import "fmt"

// writeChunkElems caps how many values one write encodes at a time: 16384
// elements, at most 128 KiB of scratch space whatever the type.
const writeChunkElems = 16384

// writeRuns encodes the selection into the file through its codec, at the runs
// the format layer computes. The caller has already validated the selection,
// matched the value count to expect and extended the record section.
func writeRuns(v *VarWriter, c codec, values any, expect int64, start, count []int64) error {
	elem := int64(c.size)
	scratch := make([]byte, writeChunkElems*elem)
	base := int64(0)
	err := v.w.h.WalkVara(v.v, start, count, func(off, n int64) error {
		for done := int64(0); done < n; {
			k := n - done
			if k > writeChunkElems {
				k = writeChunkElems
			}
			buf := scratch[:k*elem]
			if !c.encode(buf, base+done, values) {
				return v.mismatch(values)
			}
			if _, err := v.w.f.WriteAt(buf, off+done*elem); err != nil {
				return fmt.Errorf("netcdf: variable %q: writing %d bytes at offset %d: %w",
					v.v.Name, len(buf), off+done*elem, err)
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
		return fmt.Errorf("%w: variable %q: the selection wrote %d of %d values",
			ErrCorrupt, v.v.Name, base, expect)
	}
	return nil
}
