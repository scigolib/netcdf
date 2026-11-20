package netcdf

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/scigolib/netcdf/internal/cdf"
)

// Variable is one variable of a NetCDF file: its metadata and its data.
//
// Read and ReadSlice are generic over the element type, which is inferred from
// the destination slice: []int8 for byte, []byte for char and ubyte, []int16,
// []uint16, []int32, []uint32, []float32, []float64, []int64 and []uint64. The
// element type is checked at compile time; the variable's type is checked at
// run time (ErrTypeMismatch), because a file's type is discovered by opening it.
type Variable struct {
	file       *File
	v          *cdf.Var
	typ        Type
	dims       []*Dimension
	shape      []int64
	attrs      []*Attribute
	attrByName map[string]*Attribute
	length     int64
}

func newVariable(f *File, cv *cdf.Var) (*Variable, error) {
	v := &Variable{file: f, v: cv, typ: Type(cv.Type)}
	v.dims = make([]*Dimension, len(cv.DimIDs))
	for i, id := range cv.DimIDs {
		v.dims[i] = f.dims[id]
	}
	v.shape = append([]int64(nil), cv.Shape...)
	if cv.Record {
		// The record dimension's stored length is zero; its current length is
		// the file's record count.
		v.shape[0] = f.h.NumRecs
	}
	v.attrs = make([]*Attribute, len(cv.Attrs))
	v.attrByName = make(map[string]*Attribute, len(cv.Attrs))
	for i, a := range cv.Attrs {
		attr := newAttribute(a)
		v.attrs[i] = attr
		v.attrByName[a.Name] = attr
	}
	n, err := elementCount(v.shape)
	if err != nil {
		return nil, fmt.Errorf("%w: variable %q: %w", ErrCorrupt, cv.Name, err)
	}
	v.length = n
	return v, nil
}

func elementCount(shape []int64) (int64, error) {
	n := int64(1)
	for _, s := range shape {
		if s < 0 {
			return 0, errors.New("negative dimension length")
		}
		if s != 0 && n > (1<<63-1)/s {
			return 0, errors.New("element count overflows")
		}
		n *= s
	}
	return n, nil
}

// Name returns the variable's name.
func (v *Variable) Name() string { return v.v.Name }

// Type returns the variable's netCDF type.
func (v *Variable) Type() Type { return v.typ }

// Dims returns the dimensions of the variable's shape, in order.
func (v *Variable) Dims() []*Dimension {
	return append([]*Dimension(nil), v.dims...)
}

// Shape returns the variable's current shape, one length per dimension. For a
// record variable the first entry is the file's current record count.
func (v *Variable) Shape() []int64 {
	return append([]int64(nil), v.shape...)
}

// Attributes returns the variable's attributes in definition order.
func (v *Variable) Attributes() []*Attribute {
	return append([]*Attribute(nil), v.attrs...)
}

// Attribute returns the variable attribute with the given name.
func (v *Variable) Attribute(name string) (*Attribute, bool) {
	a, ok := v.attrByName[name]
	return a, ok
}

// Len returns the number of values the variable holds in the current file.
func (v *Variable) Len() int64 { return v.length }

// Read reads the whole variable into dst, which must hold exactly Len values
// of the matching Go element type. The element type is inferred from dst and
// checked against the variable's type at run time.
func (v *Variable) Read[T any](dst *[]T) error {
	return v.read(dst, make([]int64, len(v.shape)), v.Shape())
}

// ReadSlice reads a hyperslab into dst. start and count hold one value per
// dimension: the zero-based first index and the number of values along that
// dimension. The record dimension is addressed by record number.
//
// dst must hold exactly the product of count values and an element type that
// matches the variable's netCDF type. A selection outside the shape or a dst
// of the wrong length yields ErrOutOfRange; a slice of the wrong element type
// yields ErrTypeMismatch; data beyond the end of the file yields ErrCorrupt.
func (v *Variable) ReadSlice[T any](dst *[]T, start, count []int64) error {
	return v.read(dst, start, count)
}

// read is the shared erased path behind the typed methods: dst is a *[]T of
// one of the element types codecFor maps, checked against the variable's type
// here. The box lives for one call, never per element.
func (v *Variable) read(dst any, start, count []int64) error {
	if err := v.file.checkOpen(); err != nil {
		return err
	}
	expect, err := v.checkSelection(start, count)
	if err != nil {
		return err
	}
	rv := reflect.ValueOf(dst)
	if !rv.IsValid() || rv.Kind() != reflect.Pointer || rv.IsNil() {
		return errors.New("netcdf: destination must be a non-nil pointer to a slice")
	}
	c, ok := codecFor(v.typ)
	if !ok {
		return v.mismatch(dst)
	}
	have, ok := c.length(dst)
	if !ok {
		return v.mismatch(dst)
	}
	if have != expect {
		return fmt.Errorf("%w: destination holds %d values, the selection needs %d",
			ErrOutOfRange, have, expect)
	}
	return readRuns(v, c, dst, expect, start, count)
}

// checkSelection validates the hyperslab against the variable's shape and
// returns the number of values it selects.
func (v *Variable) checkSelection(start, count []int64) (int64, error) {
	if len(start) != len(v.shape) || len(count) != len(v.shape) {
		return 0, fmt.Errorf("%w: variable %q has %d dimensions, the selection has %d and %d",
			ErrOutOfRange, v.v.Name, len(v.shape), len(start), len(count))
	}
	expect := int64(1)
	for i, c := range count {
		if start[i] < 0 || c < 0 || start[i] > v.shape[i] || c > v.shape[i]-start[i] {
			return 0, fmt.Errorf("%w: variable %q dimension %d: start %d count %d, extent %d",
				ErrOutOfRange, v.v.Name, i, start[i], c, v.shape[i])
		}
		expect *= c
	}
	return expect, nil
}

func (v *Variable) mismatch(dst any) error {
	return fmt.Errorf("%w: got %T, variable %q holds %s values", ErrTypeMismatch, dst, v.v.Name, v.typ)
}
