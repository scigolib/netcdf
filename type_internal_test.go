package netcdf

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/scigolib/netcdf/internal/cdf"
)

// The package converts the internal format types with a plain cast. These
// tests keep the two constant sets and the size tables equal, so a change on
// either side fails here instead of misreading a file.
func TestTypeValuesMatchFormatLayer(t *testing.T) {
	for typ := TypeByte; typ <= TypeUInt64; typ++ {
		inner := cdf.Type(typ)
		if uint8(typ) != uint8(inner) {
			t.Errorf("Type %d converted to cdf.Type %d", typ, inner)
		}
		if got, want := typ.Size(), inner.Size(); got != want {
			t.Errorf("%v.Size() = %d, cdf.Type(%d).Size() = %d", typ, got, inner, want)
		}
	}
}

// TestErrorModel keeps the public sentinels independent of the format layer:
// go doc must never show an internal path, and translation must classify
// without leaking the internal sentinel into the public chain.
func TestErrorModel(t *testing.T) {
	for _, pair := range []struct {
		name     string
		public   error
		internal error
	}{
		{"ErrNotNetCDF", ErrNotNetCDF, cdf.ErrNotNetCDF},
		{"ErrCorrupt", ErrCorrupt, cdf.ErrCorrupt},
		{"ErrOutOfRange", ErrOutOfRange, cdf.ErrOutOfRange},
	} {
		// errorlint: the identity comparison is the point here, not a match.
		if pair.public == pair.internal { //nolint:errorlint // proving the values are distinct
			t.Errorf("%s aliases the format layer's value", pair.name)
		}
	}

	// A real classification from the format layer survives translation, and
	// the internal sentinel stays out of the public chain.
	_, err := cdf.ReadHeader(bytes.NewReader([]byte("nope")), 4)
	if err == nil {
		t.Fatal("no error for non-CDF magic")
	}
	if tr := translate(err); !errors.Is(tr, ErrNotNetCDF) {
		t.Errorf("translated magic error = %v, want ErrNotNetCDF", tr)
	}
	_, err = cdf.ReadHeader(bytes.NewReader([]byte("CDF\x01\x00\x00\x00")), 7)
	if err == nil {
		t.Fatal("no error for a truncated header")
	}
	tr := translate(err)
	if !errors.Is(tr, ErrCorrupt) {
		t.Errorf("translated error = %v, want ErrCorrupt", tr)
	}
	if errors.Is(tr, cdf.ErrCorrupt) {
		t.Error("the internal sentinel leaks into the public error chain")
	}
}

func TestCodecsMatchTypes(t *testing.T) {
	for typ := TypeByte; typ <= TypeUInt64; typ++ {
		c, ok := codecFor(typ)
		if !ok {
			t.Fatalf("no codec for %v", typ)
		}
		if c.size != typ.Size() {
			t.Errorf("%v: codec size %d, Type.Size %d", typ, c.size, typ.Size())
		}
	}
	if _, ok := codecFor(Type(12)); ok {
		t.Error("codecFor accepted NC_STRING")
	}
}

// TestCodecDecode checks each codec against hand-written external bytes,
// including the two's-complement extremes and the unsigned types CDF-5 adds.
func TestCodecDecode(t *testing.T) {
	cases := []struct {
		typ  Type
		raw  []byte
		want any
	}{
		{TypeByte, []byte{0x80, 0x7f}, []int8{-128, 127}},
		{TypeChar, []byte("ab"), []byte("ab")},
		{TypeShort, []byte{0x80, 0x01, 0x7f, 0xff}, []int16{-32767, 32767}},
		{TypeInt, []byte{0xff, 0xff, 0xff, 0xff, 0x00, 0x00, 0x00, 0x01}, []int32{-1, 1}},
		{TypeFloat, []byte{0xbf, 0xc0, 0x00, 0x00, 0x40, 0x10, 0x00, 0x00}, []float32{-1.5, 2.25}},
		{TypeDouble, []byte{
			0xbf, 0xf8, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x40, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		}, []float64{-1.5, 2.25}},
		{TypeUByte, []byte{0x00, 0xff}, []uint8{0, 255}},
		{TypeUShort, []byte{0x00, 0x01, 0xff, 0xff}, []uint16{1, 65535}},
		{TypeUInt, []byte{0x00, 0x00, 0x00, 0x01, 0xff, 0xff, 0xff, 0xff}, []uint32{1, 4294967295}},
		{TypeInt64, []byte{
			0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
		}, []int64{-1, 1}},
		{TypeUInt64, []byte{
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		}, []uint64{0, 18446744073709551615}},
	}
	for _, tc := range cases {
		c, ok := codecFor(tc.typ)
		if !ok {
			t.Fatalf("%v: no codec", tc.typ)
		}
		if got := c.value(tc.raw); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%v: value = %v, want %v", tc.typ, got, tc.want)
		}

		// Decode into the middle of a larger slice: index 0 and 3 stay zero.
		dst := reflect.New(reflect.TypeOf(tc.want))
		dst.Elem().Set(reflect.MakeSlice(reflect.TypeOf(tc.want), 4, 4))
		if !c.decode(dst.Interface(), 1, tc.raw) {
			t.Fatalf("%v: decode reported a type mismatch", tc.typ)
		}
		wantExpanded := reflect.MakeSlice(reflect.TypeOf(tc.want), 4, 4)
		wv := reflect.ValueOf(tc.want)
		for i := 0; i < wv.Len(); i++ {
			wantExpanded.Index(1 + i).Set(wv.Index(i))
		}
		if got := dst.Elem().Interface(); !reflect.DeepEqual(got, wantExpanded.Interface()) {
			t.Errorf("%v: decode at base 1 = %v, want %v", tc.typ, got, wantExpanded.Interface())
		}

		if n, ok := c.length(dst.Interface()); !ok || n != 4 {
			t.Errorf("%v: length = %d, %v, want 4 true", tc.typ, n, ok)
		}
		if _, ok := c.length(new(float64)); ok {
			t.Errorf("%v: length accepted a pointer to a non-slice", tc.typ)
		}
		if c.decode(new(float64), 0, tc.raw) {
			t.Errorf("%v: decode accepted a pointer to a non-slice", tc.typ)
		}
	}
}
