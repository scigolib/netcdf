package netcdf

import (
	"strings"

	"github.com/scigolib/netcdf/internal/cdf"
)

// Attribute is one attribute, global or attached to a variable.
//
// The values are kept in their external big-endian representation; Value
// decodes them into a typed Go slice, Bytes returns the raw bytes.
type Attribute struct {
	name string
	typ  Type
	data []byte
}

func newAttribute(a cdf.Attr) *Attribute {
	return &Attribute{name: a.Name, typ: Type(a.Type), data: a.Data}
}

// Name returns the attribute's name.
func (a *Attribute) Name() string { return a.name }

// Type returns the attribute's netCDF type.
func (a *Attribute) Type() Type { return a.typ }

// Len returns the number of values the attribute holds.
func (a *Attribute) Len() int {
	if size := a.typ.Size(); size > 0 {
		return len(a.data) / size
	}
	return 0
}

// Bytes returns the values' external (big-endian) representation, without
// header padding. The result is a copy the caller may modify.
func (a *Attribute) Bytes() []byte {
	return append([]byte(nil), a.data...)
}

// String returns a text attribute's value with trailing NUL bytes trimmed.
// It is meant for attributes of type TypeChar; for other types it returns "".
func (a *Attribute) String() string {
	if a.typ != TypeChar {
		return ""
	}
	return strings.TrimRight(string(a.data), "\x00")
}

// Value returns the attribute's values as a typed slice: []int8 for TypeByte,
// []byte for TypeChar and TypeUByte, []int16, []uint16, []int32, []uint32,
// []float32, []float64, []int64 and []uint64 for the rest.
func (a *Attribute) Value() any {
	c, ok := codecFor(a.typ)
	if !ok {
		return nil
	}
	return c.value(a.data)
}
