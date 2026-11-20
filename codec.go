package netcdf

import (
	"encoding/binary"
	"math"
)

// codec connects one netCDF type to the Go slice type its values decode into.
// It is the package's single type-mapping layer: variable reads, variable
// writes and attribute values all go through it, in both directions.
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
	// sliceLen reports how many values src (a []T of the matching slice type,
	// passed by value) holds. ok is false when src is not that type.
	sliceLen func(src any) (n int64, ok bool)
	// encode writes len(dst)/size values of src (a []T), starting at src index
	// from, into dst as external big-endian bytes. It reports false for a wrong
	// slice type or a selection beyond src.
	encode func(dst []byte, from int64, src any) bool
}

// simpleCodec builds the codec of a type whose values move between the
// external bytes and the Go type with one conversion each way.
func simpleCodec[T any](size int, conv func([]byte) T, put func([]byte, T)) codec {
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
		sliceLen: func(src any) (int64, bool) {
			sl, ok := src.([]T)
			if !ok {
				return 0, false
			}
			return int64(len(sl)), true
		},
		encode: func(dst []byte, from int64, src any) bool {
			sl, ok := src.([]T)
			if !ok || from < 0 || from+int64(len(dst)/size) > int64(len(sl)) {
				return false
			}
			for i := 0; i < len(dst)/size; i++ {
				put(dst[i*size:], sl[from+int64(i)])
			}
			return true
		},
	}
}

// codecFor returns the codec of a netCDF type. The switch is the mapping
// itself: one line per type, no logic. TypeChar and TypeUByte both decode into
// []byte; which one a variable holds is still its own Type.
func codecFor(t Type) (codec, bool) {
	switch t {
	case TypeByte:
		return simpleCodec(1, int8Value, putInt8), true
	case TypeChar:
		return simpleCodec(1, byteValue, putByte), true
	case TypeShort:
		return simpleCodec(2, int16Value, putInt16), true
	case TypeInt:
		return simpleCodec(4, int32Value, putInt32), true
	case TypeFloat:
		return simpleCodec(4, float32Value, putFloat32), true
	case TypeDouble:
		return simpleCodec(8, float64Value, putFloat64), true
	case TypeUByte:
		return simpleCodec(1, byteValue, putByte), true
	case TypeUShort:
		return simpleCodec(2, uint16Value, putUint16), true
	case TypeUInt:
		return simpleCodec(4, uint32Value, putUint32), true
	case TypeInt64:
		return simpleCodec(8, int64Value, putInt64), true
	case TypeUInt64:
		return simpleCodec(8, uint64Value, putUint64), true
	case TypeString:
		// Reserved for the enhanced model; the classic formats cannot hold it.
		return codec{}, false
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

// The encode counterparts; the signed ones write the two's-complement bits.

func putByte(b []byte, v byte)       { b[0] = v }
func putInt8(b []byte, v int8)       { b[0] = byte(v) } //nolint:gosec // two's-complement representation
func putInt16(b []byte, v int16)     { binary.BigEndian.PutUint16(b, uint16(v)) }
func putUint16(b []byte, v uint16)   { binary.BigEndian.PutUint16(b, v) }
func putInt32(b []byte, v int32)     { binary.BigEndian.PutUint32(b, uint32(v)) } //nolint:gosec // two's-complement representation
func putUint32(b []byte, v uint32)   { binary.BigEndian.PutUint32(b, v) }
func putInt64(b []byte, v int64)     { binary.BigEndian.PutUint64(b, uint64(v)) } //nolint:gosec // two's-complement representation
func putUint64(b []byte, v uint64)   { binary.BigEndian.PutUint64(b, v) }
func putFloat32(b []byte, v float32) { binary.BigEndian.PutUint32(b, math.Float32bits(v)) }
func putFloat64(b []byte, v float64) { binary.BigEndian.PutUint64(b, math.Float64bits(v)) }
