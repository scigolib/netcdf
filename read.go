package netcdf

import (
	"errors"
	"fmt"
	"io"

	"github.com/scigolib/netcdf/internal/cdf"
)

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
		// A file shorter than its header says is corruption here. netCDF-C
		// zero-fills the short part of a read (libsrc/posixio.c), which hides a
		// truncated file; reporting it is the deliberate choice.
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
			at := off + done*elem
			if err := cdf.ReadAtFull(v.file.r, buf, at); err != nil {
				if errors.Is(err, io.ErrUnexpectedEOF) {
					return fmt.Errorf("%w: variable %q: short read of %d bytes at offset %d",
						ErrCorrupt, v.v.Name, len(buf), at)
				}
				// Not corruption: pass an I/O failure through so the caller can
				// match it with errors.Is (a timeout, a closed reader).
				return fmt.Errorf("netcdf: variable %q: reading %d bytes at offset %d: %w",
					v.v.Name, len(buf), at, err)
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
