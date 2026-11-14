package netcdf

import "fmt"

// Type identifies the netCDF external data type of a variable or attribute.
// The numeric values are the type tags the classic format stores in the file.
type Type uint8

// The netCDF types. CDF-1 and CDF-2 know the first six; CDF-5 adds the
// unsigned and 64-bit ones, and NetCDF-4 adds string and user-defined types
// (which this package does not expose yet).
const (
	TypeByte   Type = 1  // NC_BYTE: 8-bit signed integer
	TypeChar   Type = 2  // NC_CHAR: text characters
	TypeShort  Type = 3  // NC_SHORT: 16-bit signed integer
	TypeInt    Type = 4  // NC_INT: 32-bit signed integer
	TypeFloat  Type = 5  // NC_FLOAT: IEEE single precision
	TypeDouble Type = 6  // NC_DOUBLE: IEEE double precision
	TypeUByte  Type = 7  // NC_UBYTE: 8-bit unsigned integer (CDF-5)
	TypeUShort Type = 8  // NC_USHORT: 16-bit unsigned integer (CDF-5)
	TypeUInt   Type = 9  // NC_UINT: 32-bit unsigned integer (CDF-5)
	TypeInt64  Type = 10 // NC_INT64: 64-bit signed integer (CDF-5)
	TypeUInt64 Type = 11 // NC_UINT64: 64-bit unsigned integer (CDF-5)
)

// String returns the type's name as CDL writes it.
func (t Type) String() string {
	switch t {
	case TypeByte:
		return "byte"
	case TypeChar:
		return "char"
	case TypeShort:
		return "short"
	case TypeInt:
		return "int"
	case TypeFloat:
		return "float"
	case TypeDouble:
		return "double"
	case TypeUByte:
		return "ubyte"
	case TypeUShort:
		return "ushort"
	case TypeUInt:
		return "uint"
	case TypeInt64:
		return "int64"
	case TypeUInt64:
		return "uint64"
	}
	return fmt.Sprintf("Type(%d)", uint8(t))
}

// Size returns the number of bytes one value occupies in the file.
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
