// Package cdf implements the byte-level reading of the classic netCDF file
// formats: CDF-1 (version byte 1, 32-bit offsets), CDF-2 (version 2, 64-bit
// offsets) and CDF-5 (version 5, 64-bit offsets and 64-bit sizes).
//
// The formats are defined by the NetCDF Classic Format Specification; the
// reference implementation is netCDF-C (libsrc/v1hpg.c, var.c, nc3internal.c).
// Every value in the header and in the data is stored big-endian, and this
// package decodes with an explicit binary.BigEndian / manual shifts only.
//
// The package is read-only: it parses a header into Header and computes how a
// hyperslab of a variable maps to file offsets. It does no conversion of data
// types; the caller decodes the bytes it asks for.
//
// Security: the input is untrusted. Every count, length and offset that comes
// from the file is validated against the bytes that remain before memory is
// allocated or a read is issued, and every arithmetic step is overflow-checked.
package cdf

import (
	"errors"
	"fmt"
)

// Format violations and input classification.
var (
	// ErrNotNetCDF reports input that does not start with a classic header.
	ErrNotNetCDF = errors.New("not a classic netCDF file")

	// ErrCorrupt reports a header or data section that violates the format.
	ErrCorrupt = errors.New("corrupt netCDF file")

	// ErrOutOfRange reports a selection outside the variable's shape.
	ErrOutOfRange = errors.New("selection out of range")
)

// kindError carries an error kind for errors.Is while its message is only the
// detail. The public package wraps it with its own sentinel, so repeating the
// internal kind's text in the message would double the prefix.
type kindError struct {
	kind error
	msg  string
}

func (e *kindError) Error() string { return e.msg }

// Unwrap reports the kind, so errors.Is finds the internal sentinel.
func (e *kindError) Unwrap() error { return e.kind }

// corruptf builds a format-violation error whose message is the detail alone.
func corruptf(format string, args ...any) error {
	return &kindError{kind: ErrCorrupt, msg: fmt.Sprintf(format, args...)}
}

// rangeFf builds a selection error whose message is the detail alone.
func rangeFf(format string, args ...any) error {
	return &kindError{kind: ErrOutOfRange, msg: fmt.Sprintf(format, args...)}
}

// notNetCDFf builds an input-classification error with a detail.
func notNetCDFf(format string, args ...any) error {
	return &kindError{kind: ErrNotNetCDF, msg: fmt.Sprintf(format, args...)}
}

// Version is the classic format version, as stored in the fourth magic byte.
type Version uint8

// The three classic versions.
const (
	Version1 Version = 1 // CDF-1, 32-bit offsets
	Version2 Version = 2 // CDF-2, 64-bit offsets
	Version5 Version = 5 // CDF-5, 64-bit offsets and 64-bit sizes
)

// String returns the format's name as the NetCDF documentation writes it.
func (v Version) String() string {
	switch v {
	case Version1:
		return "CDF-1"
	case Version2:
		return "CDF-2"
	case Version5:
		return "CDF-5"
	}
	return fmt.Sprintf("Version(%d)", uint8(v))
}

// Type is a netCDF external data type tag.
type Type uint8

// The external type tags, in the values the format stores in the header.
const (
	TypeByte   Type = 1  // NC_BYTE
	TypeChar   Type = 2  // NC_CHAR
	TypeShort  Type = 3  // NC_SHORT
	TypeInt    Type = 4  // NC_INT
	TypeFloat  Type = 5  // NC_FLOAT
	TypeDouble Type = 6  // NC_DOUBLE
	TypeUByte  Type = 7  // NC_UBYTE, CDF-5
	TypeUShort Type = 8  // NC_USHORT, CDF-5
	TypeUInt   Type = 9  // NC_UINT, CDF-5
	TypeInt64  Type = 10 // NC_INT64, CDF-5
	TypeUInt64 Type = 11 // NC_UINT64, CDF-5
)

// Size returns the number of bytes one value of the type occupies in the file.
// It returns 0 for a type tag the formats do not define.
func (t Type) Size() int {
	switch t {
	case TypeByte, TypeChar, TypeUByte:
		return 1
	case TypeShort, TypeUShort:
		return 2
	case TypeInt, TypeFloat, TypeUInt:
		return 4
	case TypeDouble, TypeInt64, TypeUInt64:
		return 8
	}
	return 0
}

// validFor reports whether the format version may carry the type tag.
// CDF-1 and CDF-2 know only the six classic types; CDF-5 adds the unsigned and
// 64-bit ones. netCDF-C accepts NC_STRING (12) in a classic header and then
// treats it as zero-sized (var.c:417 ncx_szof); the specification defines no
// such type here, so it is rejected.
func (t Type) validFor(v Version) bool {
	if t >= TypeByte && t <= TypeDouble {
		return true
	}
	return v == Version5 && t >= TypeUByte && t <= TypeUInt64
}

// typeFromTag maps a stored 32-bit type tag to its Type. The switch is the
// mapping; values outside the tags the classic formats define are rejected
// without a narrowing conversion.
func typeFromTag(code uint32) (Type, bool) {
	switch code {
	case 1:
		return TypeByte, true
	case 2:
		return TypeChar, true
	case 3:
		return TypeShort, true
	case 4:
		return TypeInt, true
	case 5:
		return TypeFloat, true
	case 6:
		return TypeDouble, true
	case 7:
		return TypeUByte, true
	case 8:
		return TypeUShort, true
	case 9:
		return TypeUInt, true
	case 10:
		return TypeInt64, true
	case 11:
		return TypeUInt64, true
	}
	return 0, false
}

// Dim is one entry of the dimensions list.
type Dim struct {
	Name string
	Size int64 // 0 marks the record (unlimited) dimension
}

// Unlimited reports whether the dimension is the record dimension.
func (d Dim) Unlimited() bool { return d.Size == 0 }

// Attr is one attribute. Data holds the values' external representation
// (big-endian, no padding), exactly Len values of Type.
type Attr struct {
	Name string
	Type Type
	Data []byte
}

// Len returns the number of values the attribute holds.
func (a Attr) Len() int {
	if n := a.Type.Size(); n > 0 {
		return len(a.Data) / n
	}
	return 0
}

// Var is one entry of the variables list, with the geometry computed and
// validated by ReadHeader.
type Var struct {
	Name   string
	DimIDs []int64 // indexes into Header.Dims
	Attrs  []Attr
	Type   Type
	Begin  int64 // file offset of the variable's data

	// Computed from DimIDs and the dimensions:
	Shape    []int64 // dimension sizes; Shape[0] == 0 for a record variable
	Strides  []int64 // element stride per dimension within one slab
	ElemSize int     // bytes per value
	Record   bool    // the first dimension is the record dimension
	DataSize int64   // bytes of values in one slab (one record, or all data)
	SlabSize int64   // bytes one record occupies in the record section
}

// Header is a parsed classic header with every variable's geometry computed.
type Header struct {
	Version Version
	NumRecs int64 // resolved record count (STREAMING is computed from the file)
	Dims    []Dim
	Attrs   []Attr // global attributes
	Vars    []Var

	HeaderSize int64 // bytes the header occupies, the first data offset
	RecSize    int64 // bytes one record occupies; 0 without record variables
}

// ReadHeader parses the header of the classic file r, whose total length is
// size. It validates the geometry of every variable (shapes, record layout,
// starting offsets) and returns ErrNotNetCDF or an error wrapping ErrCorrupt
// for input that is not a valid classic file.
//
// WalkVara maps a hyperslab of a variable to file offsets.
