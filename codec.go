package netcdf

import (
	"encoding/binary"
	"math"
)

// codec connects one netCDF type to the Go slice type its values decode into.
// It is the package's single type-mapping layer: variable reads and attribute
// values both go through it, and the writer will add the encode counterpart
// here rather than a second switch.
type codec struct {
	// size is the number of bytes one value occupies in the file.
	size int
	// length reports how many values dst (*[]T of the matching slice type)
	// holds. ok is false when dst is not that type.
	length func(dst any) (n int64, ok bool)
	// decode decodes len(b)/size values from the external big-endian bytes in
	// b into dst, starting at index base. It reports false when dst is not the
	// matching slice type.
	decode func(dst any, base int64, b []byte) bool
	// value decodes b into a fresh, exactly-sized slice.
	value func(b []byte) any
}

// simpleCodec builds the codec of a type whose values decode with one
// conversion from the external bytes.
func simpleCodec[T any](size int, conv func([]byte) T) codec {
	return codec{
		size: size,
		length: func(dst any) (int64, bool) {
			sl, ok := dst.(*[]T)
			if !ok {
				return 0, false
			}
			return int64(len(*sl)), true
		},
		decode: func(dst any, base int64, b []byte) bool {
			sl, ok := dst.(*[]T)
			if !ok {
				return false
			}
			for i := 0; i < len(b)/size; i++ {
				(*sl)[base+int64(i)] = conv(b[i*size:])
			}
			return true
		},
		value: func(b []byte) any {
			out := make([]T, len(b)/size)
			for i := range out {
				out[i] = conv(b[i*size:])
			}
			return out
		},
	}
}

// codecFor returns the codec of a netCDF type. The switch is the mapping
// itself: one line per type, no logic. TypeChar and TypeUByte both decode into
// []byte; which one a variable holds is still its own Type.
func codecFor(t Type) (codec, bool) {
	switch t {
	case TypeByte:
		return simpleCodec(1, int8Value), true
	case TypeChar:
		return simpleCodec(1, byteValue), true
	case TypeShort:
		return simpleCodec(2, int16Value), true
	case TypeInt:
		return simpleCodec(4, int32Value), true
	case TypeFloat:
		return simpleCodec(4, float32Value), true
	case TypeDouble:
		return simpleCodec(8, float64Value), true
	case TypeUByte:
		return simpleCodec(1, byteValue), true
	case TypeUShort:
		return simpleCodec(2, uint16Value), true
	case TypeUInt:
		return simpleCodec(4, uint32Value), true
	case TypeInt64:
		return simpleCodec(8, int64Value), true
	case TypeUInt64:
		return simpleCodec(8, uint64Value), true
	}
	return codec{}, false
}

// The classic formats store signed integers in two's complement, so decoding
// one is a reinterpretation of the stored bits, not a numeric conversion: the
// overflow checks of a value conversion do not apply.

func int8Bits(v byte) int8     { return int8(v) }  //nolint:gosec // two's-complement bit reinterpretation
func int16Bits(v uint16) int16 { return int16(v) } //nolint:gosec // two's-complement bit reinterpretation
func int32Bits(v uint32) int32 { return int32(v) } //nolint:gosec // two's-complement bit reinterpretation
func int64Bits(v uint64) int64 { return int64(v) } //nolint:gosec // two's-complement bit reinterpretation

// The classic formats are big-endian; every conversion spells that out.

func byteValue(b []byte) byte       { return b[0] }
func int8Value(b []byte) int8       { return int8Bits(b[0]) }
func int16Value(b []byte) int16     { return int16Bits(binary.BigEndian.Uint16(b)) }
func uint16Value(b []byte) uint16   { return binary.BigEndian.Uint16(b) }
func int32Value(b []byte) int32     { return int32Bits(binary.BigEndian.Uint32(b)) }
func uint32Value(b []byte) uint32   { return binary.BigEndian.Uint32(b) }
func int64Value(b []byte) int64     { return int64Bits(binary.BigEndian.Uint64(b)) }
func uint64Value(b []byte) uint64   { return binary.BigEndian.Uint64(b) }
func float32Value(b []byte) float32 { return math.Float32frombits(binary.BigEndian.Uint32(b)) }
func float64Value(b []byte) float64 { return math.Float64frombits(binary.BigEndian.Uint64(b)) }
