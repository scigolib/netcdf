package netcdf

// The writer: a thin handle over the file and the format layer's header. All
// format knowledge (encoding, layout, fill geometry, limits) lives in
// internal/cdf; this file converts values through codec and moves bytes.

import (
	"errors"
	"fmt"
	"os"
	"sync/atomic"

	"github.com/scigolib/netcdf/internal/cdf"
)

// Writer writes data into a file created by Create. A Writer is not safe for
// concurrent use; Close must not run concurrently with writes.
//
// The file is consistent after Close: the header's record count is written
// last, so a file abandoned before Close reads back as one with fewer records.
type Writer struct {
	f      *os.File
	h      *cdf.Header
	format Format
	fill   func(*cdf.Var) []byte

	vars   []*VarWriter
	byName map[string]*VarWriter

	closed atomic.Bool
}

func newWriter(f *os.File, h *cdf.Header, format Format, fill func(*cdf.Var) []byte) *Writer {
	w := &Writer{
		f:      f,
		h:      h,
		format: format,
		fill:   fill,
		vars:   make([]*VarWriter, len(h.Vars)),
		byName: make(map[string]*VarWriter, len(h.Vars)),
	}
	for i := range h.Vars {
		vw := &VarWriter{w: w, v: &h.Vars[i], typ: Type(h.Vars[i].Type)}
		w.vars[i] = vw
		w.byName[vw.v.Name] = vw
	}
	return w
}

// Format returns the format the file is written in.
func (w *Writer) Format() Format { return w.format }

// NumRecs returns the number of records written so far — the current length of
// the unlimited dimension.
func (w *Writer) NumRecs() int64 { return w.h.NumRecs }

// Variables returns the file's variables in definition order.
func (w *Writer) Variables() []*VarWriter {
	return append([]*VarWriter(nil), w.vars...)
}

// Variable returns the variable with the given name.
func (w *Writer) Variable(name string) (*VarWriter, bool) {
	v, ok := w.byName[name]
	return v, ok
}

// Close writes the final record count into the header and closes the file. It
// is idempotent; a write after Close reports ErrClosed.
func (w *Writer) Close() error {
	if w.closed.Swap(true) {
		return nil
	}
	err := w.h.WriteNumRecs(w.f)
	if cerr := w.f.Close(); err == nil {
		err = cerr
	}
	return err
}

func (w *Writer) checkOpen() error {
	if w.closed.Load() {
		return ErrClosed
	}
	return nil
}

// VarWriter writes one variable's values.
type VarWriter struct {
	w   *Writer
	v   *cdf.Var
	typ Type
}

// Name returns the variable's name.
func (v *VarWriter) Name() string { return v.v.Name }

// Type returns the variable's netCDF type.
func (v *VarWriter) Type() Type { return v.typ }

// Record reports whether the variable's first dimension is the unlimited one.
func (v *VarWriter) Record() bool { return v.v.Record }

// Shape returns the variable's current shape; for a record variable the first
// entry is the current record count.
func (v *VarWriter) Shape() []int64 {
	shape := append([]int64(nil), v.v.Shape...)
	if v.v.Record && len(shape) > 0 {
		shape[0] = v.w.h.NumRecs
	}
	return shape
}

// Write writes the whole variable: from the first element to the last. For a
// fixed-size variable the values must fill its shape; for a record variable
// they are written from record 0 and extend the record dimension by as many
// records as they cover.
func (v *VarWriter) Write(values any) error {
	if err := v.w.checkOpen(); err != nil {
		return err
	}
	c, ok := codecFor(v.typ)
	if !ok {
		return v.mismatch(values)
	}
	n, ok := c.sliceLen(values)
	if !ok {
		return v.mismatch(values)
	}
	if !v.v.Record {
		return v.writeSlice(values, make([]int64, len(v.v.Shape)), v.v.Shape)
	}
	// A record variable's Write appends whole records: the inner shape is
	// fixed, so the value count must be a whole number of slabs.
	slab, err := elementCount(v.v.Shape[1:])
	if err != nil {
		return fmt.Errorf("%w: variable %q: %w", ErrOutOfRange, v.v.Name, err)
	}
	if slab == 0 || n%slab != 0 {
		return fmt.Errorf("%w: variable %q: %d values do not fill whole records of %d",
			ErrOutOfRange, v.v.Name, n, slab)
	}
	count := append([]int64(nil), v.v.Shape...)
	count[0] = n / slab
	return v.writeSlice(values, make([]int64, len(count)), count)
}

// WriteSlice writes a hyperslab. start and count hold one value per dimension:
// the zero-based first index and the number written. For a record variable the
// first dimension is addressed by record number; writing past the current
// record count fills and appends the records in between with the fill value,
// so a start far beyond the current count costs a fill of every record in
// between — the same behavior as netCDF-C's NCvnrecs.
func (v *VarWriter) WriteSlice(values any, start, count []int64) error {
	if err := v.w.checkOpen(); err != nil {
		return err
	}
	return v.writeSlice(values, start, count)
}

// writeSlice is the shared path of Write and WriteSlice; the caller has
// already checked the Writer is open.
func (v *VarWriter) writeSlice(values any, start, count []int64) error {
	rank := len(v.v.Shape)
	if len(start) != rank || len(count) != rank {
		return fmt.Errorf("%w: variable %q has %d dimensions, the selection has %d and %d",
			ErrOutOfRange, v.v.Name, rank, len(start), len(count))
	}
	// The selection is validated before anything changes: a rejected write
	// must not extend or fill the record section (netCDF-C's NCcoordck and
	// NCedgeck run before NCvnrecs for the same reason).
	if err := v.w.h.CheckWriteSelection(v.v, start, count); err != nil {
		return translate(err)
	}
	expect, err := elementCount(count)
	if err != nil {
		return fmt.Errorf("%w: variable %q: %w", ErrOutOfRange, v.v.Name, err)
	}
	c, ok := codecFor(v.typ)
	if !ok {
		return v.mismatch(values)
	}
	have, ok := c.sliceLen(values)
	if !ok {
		return v.mismatch(values)
	}
	if have != expect {
		return fmt.Errorf("%w: variable %q: %d values given, the selection needs %d",
			ErrOutOfRange, v.v.Name, have, expect)
	}
	if err := v.extendIfNeeded(start, count); err != nil {
		return err
	}
	return writeRuns(v, c, values, expect, start, count)
}

// extendIfNeeded fills and appends the records a write reaches past the
// current count (NCfillrecord semantics), so the walk below addresses them
// normally.
func (v *VarWriter) extendIfNeeded(start, count []int64) error {
	if !v.v.Record || count[0] == 0 {
		return nil
	}
	needed, err := addCount(start[0], count[0])
	if err != nil {
		return fmt.Errorf("%w: variable %q: %w", ErrOutOfRange, v.v.Name, err)
	}
	if needed <= v.w.h.NumRecs {
		return nil
	}
	// An unaddressable count is mapped to ErrOutOfRange by translate; a real
	// write error (a full disk, an I/O failure) passes through as itself.
	if err := v.w.h.ExtendRecs(v.w.f, needed, v.w.fill); err != nil {
		return translate(err)
	}
	return nil
}

// addCount adds two non-negative selection coordinates, reporting overflow.
func addCount(a, b int64) (int64, error) {
	if a < 0 || b < 0 {
		return 0, errors.New("negative coordinate")
	}
	if a > 1<<63-1-b {
		return 0, errors.New("record number overflows")
	}
	return a + b, nil
}

func (v *VarWriter) mismatch(values any) error {
	return fmt.Errorf("%w: got %T, variable %q holds %s values", ErrTypeMismatch, values, v.v.Name, v.typ)
}
