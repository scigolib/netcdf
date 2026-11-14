package netcdf

// Schema describes a NetCDF file to create. It collects dimensions, attributes
// and variables; Create validates the whole schema, lays the file out exactly
// as netCDF-C does and writes it. The format belongs to the schema, because it
// decides the header's field widths and which types are legal.
//
// A Schema is a builder, not a live object: validate happens once, in Create,
// so a schema cannot produce an invalid header halfway (ADR-002).

import (
	"errors"
	"fmt"
	"os"

	"github.com/scigolib/netcdf/internal/cdf"
)

// ErrInvalidSchema reports a schema Create cannot write: an unknown format, an
// illegal or duplicated name, a type the format does not have, a variable whose
// dimensions are not the schema's, more than one unlimited dimension, an
// invalid _FillValue, or a layout the format cannot address.
var ErrInvalidSchema = errors.New("invalid netCDF schema")

// Schema collects the definitions of one file to create.
type Schema struct {
	format Format
	dims   []*SchemaDim
	attrs  []*Attribute
	vars   []*SchemaVar

	err error // the first builder error; Create reports it
}

// NewSchema returns an empty schema for the given format: FormatClassic is
// CDF-1, FormatClassic64 is CDF-2 and FormatClassic64Data is CDF-5.
func NewSchema(format Format) *Schema {
	return &Schema{format: format}
}

// Format returns the format the schema will be written in.
func (s *Schema) Format() Format { return s.format }

// SchemaDim is one dimension of a schema. A length of zero marks the unlimited
// (record) dimension; at most one dimension may be unlimited.
type SchemaDim struct {
	name   string
	length int64
}

// AddDimension adds a dimension. It returns the handle AddVariable takes; the
// name and the length are validated by Create.
func (s *Schema) AddDimension(name string, length int64) *SchemaDim {
	d := &SchemaDim{name: name, length: length}
	s.dims = append(s.dims, d)
	return d
}

// Dimensions returns the schema's dimensions in definition order.
func (s *Schema) Dimensions() []*SchemaDim {
	return append([]*SchemaDim(nil), s.dims...)
}

// Name returns the dimension's name.
func (d *SchemaDim) Name() string { return d.name }

// Len returns the dimension's defined length; the unlimited dimension is 0.
func (d *SchemaDim) Len() int64 { return d.length }

// Unlimited reports whether the dimension is the record (unlimited) one.
func (d *SchemaDim) Unlimited() bool { return d.length == 0 }

// SchemaVar is one variable of a schema, with its attributes.
type SchemaVar struct {
	schema *Schema
	name   string
	typ    Type
	dims   []*SchemaDim
	attrs  []*Attribute
}

// AddVariable adds a variable over the given dimensions. The dimensions must
// be handles this schema returned (Create reports a foreign one); the first
// dimension may be the unlimited one.
func (s *Schema) AddVariable(name string, typ Type, dims ...*SchemaDim) *SchemaVar {
	v := &SchemaVar{schema: s, name: name, typ: typ, dims: append([]*SchemaDim(nil), dims...)}
	s.vars = append(s.vars, v)
	return v
}

// Variables returns the schema's variables in definition order.
func (s *Schema) Variables() []*SchemaVar {
	return append([]*SchemaVar(nil), s.vars...)
}

// Name returns the variable's name.
func (v *SchemaVar) Name() string { return v.name }

// Type returns the variable's netCDF type.
func (v *SchemaVar) Type() Type { return v.typ }

// Dims returns the variable's dimensions in order.
func (v *SchemaVar) Dims() []*SchemaDim {
	return append([]*SchemaDim(nil), v.dims...)
}

// Attributes returns the variable's attributes in definition order.
func (v *SchemaVar) Attributes() []*Attribute {
	return append([]*Attribute(nil), v.attrs...)
}

// SetAttr sets a global attribute, inferring the netCDF type from the Go
// value: string and []byte are char, []int8 byte, []int16 short, []int32 int,
// []float32 float, []float64 double, []uint16 ushort, []uint32 uint, []int64
// int64 and []uint64 uint64. Setting an existing name replaces its value in
// place. The error is also recorded in the schema, so Create fails even if the
// caller ignores it.
func (s *Schema) SetAttr(name string, values any) error {
	return s.setAttr(&s.attrs, name, values)
}

// SetAttrTyped sets a global attribute with an explicit netCDF type, for the
// cases inference cannot tell apart (ubyte vs char, uint vs int).
func (s *Schema) SetAttrTyped(name string, typ Type, values any) error {
	return s.setAttrTyped(&s.attrs, name, typ, values)
}

// SetAttr sets a variable attribute; see Schema.SetAttr.
func (v *SchemaVar) SetAttr(name string, values any) error {
	return v.schema.setAttr(&v.attrs, name, values)
}

// SetAttrTyped sets a variable attribute with an explicit type; see
// Schema.SetAttrTyped.
func (v *SchemaVar) SetAttrTyped(name string, typ Type, values any) error {
	return v.schema.setAttrTyped(&v.attrs, name, typ, values)
}

// Create writes a new file at path from schema. It validates the schema, lays
// the header out exactly as netCDF-C's NC_begins does, writes the header and
// fills every fixed-size variable with its fill value.
//
// A schema error is reported before the file is touched; the returned Writer
// is usable until Close.
func Create(path string, schema *Schema) (*Writer, error) {
	if schema == nil {
		return nil, fmt.Errorf("%w: nil schema", ErrInvalidSchema)
	}
	if schema.err != nil {
		return nil, schema.err
	}
	h, err := schema.header()
	if err != nil {
		return nil, err
	}
	if err := h.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidSchema, err.Error())
	}
	if err := h.Layout(); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidSchema, err.Error())
	}
	fill, err := schema.fillFunc(h)
	if err != nil {
		return nil, err
	}
	// Creating the caller's path is this function's purpose; the path is an
	// argument, not an untrusted include.
	f, err := os.Create(path) //nolint:gosec // G304: the path is this function's argument by design
	if err != nil {
		return nil, err
	}
	if err := h.WriteHeader(f); err != nil {
		_ = f.Close()
		_ = os.Remove(path) // the truncate already destroyed the old content
		return nil, err
	}
	if err := h.FillFixed(f, fill); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return nil, err
	}
	return newWriter(f, h, schema.format, fill), nil
}

// setAttr infers the attribute's netCDF type from the Go value.
func (s *Schema) setAttr(dst *[]*Attribute, name string, values any) error {
	typ, ok := inferAttrType(values)
	if !ok {
		return s.fail(fmt.Errorf("%w: attribute %q: a value of type %T has no netCDF type",
			ErrTypeMismatch, name, values))
	}
	return s.setAttrTyped(dst, name, typ, values)
}

// setAttrTyped records the value with an explicit type. The encoding is the
// codec's, the same table reads use.
func (s *Schema) setAttrTyped(dst *[]*Attribute, name string, typ Type, values any) error {
	if err := cdf.CheckName(name); err != nil {
		return s.fail(fmt.Errorf("%w: %s", ErrInvalidSchema, err.Error()))
	}
	data, err := encodeAttr(typ, values)
	if err != nil {
		return s.fail(err)
	}
	for _, a := range *dst {
		if a.name == name {
			a.typ, a.data = typ, data
			return nil
		}
	}
	*dst = append(*dst, &Attribute{name: name, typ: typ, data: data})
	return nil
}

// fail records the first builder error: Create reports it even when the caller
// ignored the method's error.
func (s *Schema) fail(err error) error {
	if s.err == nil {
		s.err = err
	}
	return err
}

// inferAttrType maps a Go value to the netCDF type it is written as.
// []byte is text (char); SetAttrTyped writes an unsigned byte attribute.
func inferAttrType(values any) (Type, bool) {
	switch values.(type) {
	case string, []byte:
		return TypeChar, true
	case []int8:
		return TypeByte, true
	case []int16:
		return TypeShort, true
	case []int32:
		return TypeInt, true
	case []float32:
		return TypeFloat, true
	case []float64:
		return TypeDouble, true
	case []uint16:
		return TypeUShort, true
	case []uint32:
		return TypeUInt, true
	case []int64:
		return TypeInt64, true
	case []uint64:
		return TypeUInt64, true
	}
	return 0, false
}

// encodeAttr turns values into their external big-endian representation.
func encodeAttr(typ Type, values any) ([]byte, error) {
	if typ == TypeChar {
		switch v := values.(type) {
		case string:
			return []byte(v), nil
		case []byte:
			return append([]byte(nil), v...), nil
		}
	}
	c, ok := codecFor(typ)
	if !ok {
		return nil, fmt.Errorf("%w: attribute type tag %d", ErrInvalidSchema, uint8(typ))
	}
	n, ok := c.sliceLen(values)
	if !ok {
		return nil, fmt.Errorf("%w: attribute of type %s cannot take a %T",
			ErrTypeMismatch, typ, values)
	}
	if n == 0 {
		return nil, nil
	}
	maxInt := int64(^uint(0) >> 1)
	if n > maxInt/int64(c.size) {
		return nil, fmt.Errorf("%w: attribute of %d values overflows the address space", ErrInvalidSchema, n)
	}
	data := make([]byte, n*int64(c.size))
	if !c.encode(data, 0, values) {
		return nil, fmt.Errorf("%w: attribute of type %s cannot take a %T",
			ErrTypeMismatch, typ, values)
	}
	return data, nil
}

// versionForFormat maps the public format to the classic version byte.
func versionForFormat(f Format) (cdf.Version, error) {
	switch f {
	case FormatClassic:
		return cdf.Version1, nil
	case FormatClassic64:
		return cdf.Version2, nil
	case FormatClassic64Data:
		return cdf.Version5, nil
	case FormatHDF5:
		return 0, fmt.Errorf("%w: writing NetCDF-4 files is not implemented", ErrUnsupportedFormat)
	case FormatUnknown:
		return 0, fmt.Errorf("%w: format %v cannot be created", ErrInvalidSchema, f)
	}
	return 0, fmt.Errorf("%w: format %v cannot be created", ErrInvalidSchema, f)
}

// header turns the collected definitions into the format layer's model.
func (s *Schema) header() (*cdf.Header, error) {
	version, err := versionForFormat(s.format)
	if err != nil {
		return nil, err
	}
	h := &cdf.Header{Version: version}
	dimIndex := make(map[*SchemaDim]int64, len(s.dims))
	for i, d := range s.dims {
		h.Dims = append(h.Dims, cdf.Dim{Name: d.name, Size: d.length})
		dimIndex[d] = int64(i)
	}
	h.Attrs = convertAttrs(s.attrs)
	for _, sv := range s.vars {
		ids := make([]int64, len(sv.dims))
		for j, d := range sv.dims {
			id, ok := dimIndex[d]
			if !ok {
				return nil, fmt.Errorf("%w: variable %q uses a dimension of another schema",
					ErrInvalidSchema, sv.name)
			}
			ids[j] = id
		}
		h.Vars = append(h.Vars, cdf.Var{
			Name:   sv.name,
			DimIDs: ids,
			Attrs:  convertAttrs(sv.attrs),
			Type:   cdf.Type(sv.typ),
		})
	}
	return h, nil
}

func convertAttrs(as []*Attribute) []cdf.Attr {
	if len(as) == 0 {
		return nil
	}
	out := make([]cdf.Attr, len(as))
	for i, a := range as {
		out[i] = cdf.Attr{Name: a.name, Type: cdf.Type(a.typ), Data: a.data}
	}
	return out
}

// fillFunc resolves each variable's fill bytes: its _FillValue attribute when
// present, the type's default otherwise. Validate has already proven the
// types, so the defaults cannot fail here.
func (s *Schema) fillFunc(h *cdf.Header) (func(*cdf.Var) []byte, error) {
	defaults := make(map[cdf.Type][]byte)
	overrides := make(map[string][]byte, len(h.Vars))
	for i := range h.Vars {
		v := &h.Vars[i]
		if _, ok := defaults[v.Type]; !ok {
			b, err := cdf.FillBytes(v.Type)
			if err != nil {
				return nil, fmt.Errorf("%w: %s", ErrInvalidSchema, err.Error())
			}
			defaults[v.Type] = b
		}
		for _, a := range v.Attrs {
			if a.Name == "_FillValue" {
				overrides[v.Name] = a.Data
				break
			}
		}
	}
	return func(v *cdf.Var) []byte {
		if b, ok := overrides[v.Name]; ok {
			return b
		}
		return defaults[v.Type]
	}, nil
}
