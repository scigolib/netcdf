package cdf

import "errors"

// WalkVara calls fn once per contiguous run of values in the requested
// hyperslab, in row-major order, with the run's file offset and its length in
// values. start and count must have one entry per dimension of v; the record
// dimension of a record variable is addressed by record number.
//
// A selection outside the variable's shape, including a negative start or
// count, yields ErrOutOfRange. WalkVara does not check the runs against the
// file length: the caller knows the file and must bounds-check before reading.
func (h *Header) WalkVara(v *Var, start, count []int64, fn func(off, n int64) error) error {
	if fn == nil {
		return errors.New("cdf: nil walk function")
	}
	empty, err := h.checkSelection(v, start, count)
	if err != nil {
		return err
	}
	if empty {
		return nil
	}
	if len(v.Shape) == 0 {
		return fn(v.Begin, 1)
	}
	if v.Record {
		// Records are addressed through the record size; a run never spans
		// two record slabs.
		for r := int64(0); r < count[0]; r++ {
			base := v.Begin + (start[0]+r)*h.RecSize
			if err := walkSlab(v, start, count, 1, base, fn); err != nil {
				return err
			}
		}
		return nil
	}
	return walkSlab(v, start, count, 0, v.Begin, fn)
}

// checkSelection validates every dimension of the hyperslab against the
// variable's extent, then reports an empty selection (a zero count) so no read
// is issued at all. A bad dimension after a zero count is still an error, as
// in netCDF-C's NCedgeck; an empty selection is not a license to go unchecked.
func (h *Header) checkSelection(v *Var, start, count []int64) (empty bool, err error) {
	rank := len(v.Shape)
	if len(start) != rank || len(count) != rank {
		return false, rangeFf("selection has %d and %d dimensions, variable %q has %d",
			len(start), len(count), v.Name, rank)
	}
	for i := range v.Shape {
		extent := v.Shape[i]
		if v.Record && i == 0 {
			extent = h.NumRecs
		}
		if start[i] < 0 || count[i] < 0 || start[i] > extent || count[i] > extent-start[i] {
			return false, rangeFf("variable %q dimension %d: start %d count %d, extent %d",
				v.Name, i, start[i], count[i], extent)
		}
		if count[i] == 0 {
			empty = true
		}
	}
	return empty, nil
}

// CheckWriteSelection validates a hyperslab for writing: the inner dimensions
// against the variable's shape, the record dimension only for overflow, since
// a write may extend the record section. The caller extends first and then
// walks with WalkVara, which revalidates against the new record count.
func (h *Header) CheckWriteSelection(v *Var, start, count []int64) error {
	rank := len(v.Shape)
	if len(start) != rank || len(count) != rank {
		return rangeFf("selection has %d and %d dimensions, variable %q has %d",
			len(start), len(count), v.Name, rank)
	}
	for i := range v.Shape {
		if start[i] < 0 || count[i] < 0 {
			return rangeFf("variable %q dimension %d: start %d count %d",
				v.Name, i, start[i], count[i])
		}
		if v.Record && i == 0 {
			if _, err := addCheck(start[i], count[i]); err != nil {
				return rangeFf("variable %q dimension %d: %s", v.Name, i, err.Error())
			}
			continue
		}
		if start[i] > v.Shape[i] || count[i] > v.Shape[i]-start[i] {
			return rangeFf("variable %q dimension %d: start %d count %d, extent %d",
				v.Name, i, start[i], count[i], v.Shape[i])
		}
	}
	return nil
}

// contiguousFrom returns the first dimension of the selection [from, rank)
// whose values the walk can read as one run. The innermost dimension always is
// one run: its stride is one. A dimension outside it joins the run only while
// it is fully selected and every dimension inside it is too, because any
// partial selection leaves gaps in the file.
func contiguousFrom(v *Var, start, count []int64, from int) int {
	last := len(count) - 1
	if last < from {
		return len(count)
	}
	if start[last] != 0 || count[last] != v.Shape[last] {
		return last
	}
	for d := last - 1; d >= from; d-- {
		if start[d] != 0 || count[d] != v.Shape[d] {
			return last
		}
		last = d
	}
	return last
}

// walkSlab emits the contiguous runs of the dimensions [from, rank) of the
// selection, all within one slab whose first value is at base.
func walkSlab(v *Var, start, count []int64, from int, base int64, fn func(off, n int64) error) error {
	coalesceFrom := contiguousFrom(v, start, count, from)
	runLen := int64(1)
	for i := coalesceFrom; i < len(count); i++ {
		runLen *= count[i]
	}
	elem := int64(v.ElemSize)
	// Every dimension inside the run but the first is fully selected from
	// zero; the first one may start anywhere, so its offset is applied here.
	runStart := int64(0)
	if coalesceFrom < len(count) {
		runStart = start[coalesceFrom] * v.Strides[coalesceFrom] * elem
	}

	var walk func(d int, off int64) error
	walk = func(d int, off int64) error {
		if d == coalesceFrom {
			return fn(base+off+runStart, runLen)
		}
		stride := v.Strides[d] * elem
		for i := int64(0); i < count[d]; i++ {
			if err := walk(d+1, off+(start[d]+i)*stride); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(from, 0)
}
