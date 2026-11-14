package cdf

import (
	"bytes"
	"errors"
	"io"
	"math"
	"testing"
)

// memWriterAt is a growable in-memory io.WriterAt for fill tests.
type memWriterAt struct{ b []byte }

func (m *memWriterAt) WriteAt(p []byte, off int64) (int, error) {
	need := int(off) + len(p)
	if need > len(m.b) {
		grown := make([]byte, need)
		copy(grown, m.b)
		m.b = grown
	}
	copy(m.b[off:], p)
	return len(p), nil
}

// errWriterAt fails every write.
type errWriterAt struct{}

func (errWriterAt) WriteAt([]byte, int64) (int, error) { return 0, io.ErrShortWrite }

func defaultFill(v *Var) []byte {
	b, err := FillBytes(v.Type)
	if err != nil {
		panic(err)
	}
	return b
}

// TestValidateAcceptsFixtures proves that every header the parser accepts from
// the ncgen fixtures also passes the writer's rules (a new file has NumRecs 0).
func TestValidateAcceptsFixtures(t *testing.T) {
	for _, name := range writerFixtures {
		t.Run(name, func(t *testing.T) {
			h, _ := readFixture(t, name)
			h.NumRecs = 0
			if err := h.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}
		})
	}
}

// TestValidateRejects walks the schema rules the format layer enforces.
func TestValidateRejects(t *testing.T) {
	dim := func(name string, size int64) Dim { return Dim{Name: name, Size: size} }
	attr := func(name string, typ Type, data []byte) Attr { return Attr{Name: name, Type: typ, Data: data} }
	cases := []struct {
		name string
		h    Header
	}{
		{"unknown version", Header{Version: 3}},
		{"records on a new file", Header{Version: Version1, NumRecs: 2}},
		{"duplicate dimension", Header{Version: Version1, Dims: []Dim{dim("x", 2), dim("x", 2)}}},
		{"negative dimension", Header{Version: Version1, Dims: []Dim{dim("x", -1)}}},
		{"two unlimited dimensions", Header{Version: Version1, Dims: []Dim{dim("t", 0), dim("u", 0)}}},
		{"bad dimension name", Header{Version: Version1, Dims: []Dim{dim("a/b", 2)}}},
		{"duplicate variable", Header{
			Version: Version1, Dims: []Dim{dim("x", 2)},
			Vars: []Var{{Name: "v", Type: TypeInt, DimIDs: []int64{0}}, {Name: "v", Type: TypeInt, DimIDs: []int64{0}}},
		}},
		{"missing dimension", Header{
			Version: Version1, Vars: []Var{{Name: "v", Type: TypeInt, DimIDs: []int64{0}}},
		}},
		{"unlimited not first", Header{
			Version: Version1, Dims: []Dim{dim("x", 2), dim("t", 0)},
			Vars: []Var{{Name: "v", Type: TypeInt, DimIDs: []int64{0, 1}}},
		}},
		{"type not in format", Header{
			Version: Version1, Dims: []Dim{dim("x", 2)},
			Vars: []Var{{Name: "v", Type: TypeUByte, DimIDs: []int64{0}}},
		}},
		{"dimension above the 32-bit size_t", Header{
			Version: Version1, Dims: []Dim{dim("x", 1<<33)},
		}},
		{"duplicate attribute", Header{
			Version: Version1,
			Attrs:   []Attr{attr("a", TypeInt, []byte{0, 0, 0, 1}), attr("a", TypeInt, []byte{0, 0, 0, 2})},
		}},
		{"attribute type not in format", Header{
			Version: Version1, Attrs: []Attr{attr("a", TypeUInt, []byte{0, 0, 0, 1})},
		}},
		{"attribute holds a partial value", Header{
			Version: Version1, Attrs: []Attr{attr("a", TypeInt, []byte{0, 0, 0})},
		}},
		{"fill value of another type", Header{
			Version: Version1, Dims: []Dim{dim("x", 2)},
			Vars: []Var{{Name: "v", Type: TypeInt, DimIDs: []int64{0},
				Attrs: []Attr{attr("_FillValue", TypeDouble, make([]byte, 8))}}},
		}},
		{"fill value with two values", Header{
			Version: Version1, Dims: []Dim{dim("x", 2)},
			Vars: []Var{{Name: "v", Type: TypeInt, DimIDs: []int64{0},
				Attrs: []Attr{attr("_FillValue", TypeInt, make([]byte, 8))}}},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.h.Validate(); err == nil {
				t.Fatal("Validate accepted an invalid header")
			}
		})
	}
}

// TestLayoutErrors covers the layout's own failures, including the format's
// addressability limits (NC_check_vlens) and the CDF-1 offset limit.
func TestLayoutErrors(t *testing.T) {
	cases := []struct {
		name string
		h    Header
	}{
		{"unknown version", Header{Version: 7}},
		{"dimension id out of range", Header{
			Version: Version1, Vars: []Var{{Name: "v", Type: TypeInt, DimIDs: []int64{0}}},
		}},
		{"negative dimension id", Header{
			Version: Version1, Vars: []Var{{Name: "v", Type: TypeInt, DimIDs: []int64{-1}}},
		}},
		{"cdf1 offset beyond the range", Header{
			Version: Version1, Dims: []Dim{{Name: "x", Size: 2147483640}},
			Vars: []Var{
				{Name: "a", Type: TypeByte, DimIDs: []int64{0}},
				{Name: "b", Type: TypeByte, DimIDs: []int64{0}},
			},
		}},
		{"cdf2 two too-large variables", Header{
			Version: Version2, Dims: []Dim{{Name: "x", Size: 4294967293}},
			Vars: []Var{
				{Name: "a", Type: TypeByte, DimIDs: []int64{0}},
				{Name: "b", Type: TypeByte, DimIDs: []int64{0}},
			},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.h.Layout(); err == nil {
				t.Fatal("Layout accepted an impossible header")
			}
		})
	}
}

// TestFillFixedAndRecords proves the fill geometry: fixed variables fill their
// padded data size, record variables fill each record's slab, including the
// single-variable case's unpadded records.
func TestFillFixedAndRecords(t *testing.T) {
	h := &Header{Version: Version1, Dims: []Dim{
		{Name: "t", Size: 0},
		{Name: "n", Size: 3},
	}}
	h.Vars = []Var{
		{Name: "b", Type: TypeByte, DimIDs: []int64{1}},
		{Name: "rec", Type: TypeShort, DimIDs: []int64{0}},
	}
	if err := h.Layout(); err != nil {
		t.Fatalf("Layout: %v", err)
	}
	var mem memWriterAt
	if err := h.FillFixed(&mem, defaultFill); err != nil {
		t.Fatalf("FillFixed: %v", err)
	}
	begin := h.Vars[0].Begin
	if got, want := mem.b[begin:begin+4], []byte{0x81, 0x81, 0x81, 0x81}; !bytes.Equal(got, want) {
		t.Fatalf("fixed fill = % x, want % x", got, want)
	}
	if err := h.ExtendRecs(&mem, 2, defaultFill); err != nil {
		t.Fatalf("ExtendRecs: %v", err)
	}
	if h.NumRecs != 2 {
		t.Fatalf("NumRecs = %d, want 2", h.NumRecs)
	}
	rec := h.Vars[1].Begin
	if got, want := mem.b[rec:rec+4], []byte{0x80, 0x01, 0x80, 0x01}; !bytes.Equal(got, want) {
		t.Fatalf("record 0 fill = % x, want % x", got, want)
	}
}

// TestFillRecordsSingleVariable covers the specification's packed case: one
// record variable, no padding between records.
func TestFillRecordsSingleVariable(t *testing.T) {
	h := &Header{Version: Version1, Dims: []Dim{{Name: "t", Size: 0}}}
	h.Vars = []Var{{Name: "b", Type: TypeByte, DimIDs: []int64{0}}}
	if err := h.Layout(); err != nil {
		t.Fatalf("Layout: %v", err)
	}
	if h.RecSize != 1 {
		t.Fatalf("RecSize = %d, want the unpadded 1", h.RecSize)
	}
	var mem memWriterAt
	if err := h.ExtendRecs(&mem, 3, defaultFill); err != nil {
		t.Fatalf("ExtendRecs: %v", err)
	}
	if got, want := mem.b[h.Vars[0].Begin:], []byte{0x81, 0x81, 0x81}; !bytes.Equal(got, want) {
		t.Fatalf("record bytes = % x, want % x", got, want)
	}
}

// TestExtendRecsLimits covers the record count limits and the write failures.
func TestExtendRecsLimits(t *testing.T) {
	h := &Header{Version: Version1, Dims: []Dim{{Name: "t", Size: 0}}}
	h.Vars = []Var{{Name: "b", Type: TypeByte, DimIDs: []int64{0}}}
	if err := h.Layout(); err != nil {
		t.Fatalf("Layout: %v", err)
	}
	// CDF-1 and CDF-2 hold a 32-bit NON_NEG whose 0xFFFFFFFF value is the
	// STREAMING marker; 2^32-1 is therefore the first count the format cannot
	// store, and the error is a selection error, not an I/O one.
	err := h.ExtendRecs(&memWriterAt{}, math.MaxUint32, defaultFill)
	if err == nil {
		t.Fatal("ExtendRecs accepted a CDF-1 record count above the limit")
	}
	if !errors.Is(err, ErrOutOfRange) {
		t.Errorf("limit error = %v, want the ErrOutOfRange kind", err)
	}
	if h.NumRecs != 0 {
		t.Errorf("a refused extension changed NumRecs to %d", h.NumRecs)
	}
	// A header with a fixed variable is needed for the write-error path.
	fixed := &Header{Version: Version1, Dims: []Dim{{Name: "n", Size: 3}}}
	fixed.Vars = []Var{{Name: "b", Type: TypeByte, DimIDs: []int64{0}}}
	if err := fixed.Layout(); err != nil {
		t.Fatalf("Layout: %v", err)
	}
	if err := fixed.FillFixed(errWriterAt{}, defaultFill); err == nil {
		t.Error("FillFixed hid a write error")
	}
	if err := fixed.FillFixed(nil, defaultFill); err == nil {
		t.Error("FillFixed accepted a nil writer")
	}
	if err := fixed.FillFixed(&memWriterAt{}, nil); err == nil {
		t.Error("FillFixed accepted a nil fill function")
	}
	if err := fixed.FillRecords(&memWriterAt{}, 2, 1, defaultFill); err == nil {
		t.Error("FillRecords accepted an inverted range")
	}
}

// TestWriteNumRecs proves the count lands where the parser reads it, in both
// field widths.
func TestWriteNumRecs(t *testing.T) {
	for _, v := range []Version{Version1, Version2, Version5} {
		h := &Header{Version: v, NumRecs: 3}
		var mem memWriterAt
		if err := h.WriteNumRecs(&mem); err != nil {
			t.Fatalf("%s: WriteNumRecs: %v", v, err)
		}
		want := []byte{0, 0, 0, 3}
		if v == Version5 {
			want = []byte{0, 0, 0, 0, 0, 0, 0, 3}
		}
		if got := mem.b[4 : 4+len(want)]; !bytes.Equal(got, want) {
			t.Errorf("%s: bytes at 4 = % x, want % x", v, got, want)
		}
	}
	h := &Header{Version: Version1, NumRecs: -1}
	if err := h.WriteNumRecs(&memWriterAt{}); err == nil {
		t.Error("WriteNumRecs accepted a negative count")
	}
	if err := h.WriteNumRecs(nil); err == nil {
		t.Error("WriteNumRecs accepted a nil writer")
	}
}

// TestWriteHeaderVsizeSentinel covers the specification's 2^32-1 marker: a
// last fixed variable whose padded size exceeds 2^32-4 stores the sentinel in
// vsize, exactly as netCDF-C does (v1hpg.c:993-1004).
func TestWriteHeaderVsizeSentinel(t *testing.T) {
	for _, version := range []Version{Version1, Version2} {
		t.Run(version.String(), func(t *testing.T) {
			h := &Header{Version: version, Dims: []Dim{
				{Name: "a", Size: 65536},
				{Name: "b", Size: 65536},
			}}
			h.Vars = []Var{{Name: "big", Type: TypeByte, DimIDs: []int64{0, 1}}}
			if err := h.Layout(); err != nil {
				t.Fatalf("Layout: %v", err)
			}
			var buf bytes.Buffer
			if err := h.WriteHeader(&buf); err != nil {
				t.Fatalf("WriteHeader: %v", err)
			}
			// vsize is the 4-byte field immediately before the begin.
			vsizeAt := int(h.HeaderSize) - int(ofsSize(version)) - 4
			if got := buf.Bytes()[vsizeAt : vsizeAt+4]; !bytes.Equal(got, []byte{0xff, 0xff, 0xff, 0xff}) {
				t.Fatalf("vsize = % x, want the 2^32-1 sentinel", got)
			}
			// The parser ignores vsize and accepts the header.
			parsed, err := ReadHeader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
			if err != nil {
				t.Fatalf("ReadHeader: %v", err)
			}
			if parsed.Vars[0].DataSize != 1<<32 {
				t.Fatalf("parsed data size = %d, want 2^32", parsed.Vars[0].DataSize)
			}
		})
	}
}

// TestRecordCountLimits pins both sides of the CDF-1/2 bound without filling
// gigabytes: 2^32-2 is storable, 2^32-1 is the reserved STREAMING marker.
func TestRecordCountLimits(t *testing.T) {
	h := &Header{Version: Version1, Dims: []Dim{{Name: "t", Size: 0}}}
	h.Vars = []Var{{Name: "b", Type: TypeByte, DimIDs: []int64{0}}}
	if err := h.Layout(); err != nil {
		t.Fatalf("Layout: %v", err)
	}
	h.NumRecs = math.MaxUint32 - 1
	var buf bytes.Buffer
	if err := h.WriteHeader(&buf); err != nil {
		t.Fatalf("WriteHeader at 2^32-2: %v", err)
	}
	var mem memWriterAt
	if err := h.WriteNumRecs(&mem); err != nil {
		t.Fatalf("WriteNumRecs at 2^32-2: %v", err)
	}
	if got := mem.b[4:8]; !bytes.Equal(got, []byte{0xff, 0xff, 0xff, 0xfe}) {
		t.Fatalf("stored count = % x, want 2^32-2", got)
	}
	h.NumRecs = math.MaxUint32
	if err := h.WriteHeader(&bytes.Buffer{}); err == nil {
		t.Error("WriteHeader accepted the STREAMING marker as a count")
	}
	if err := h.WriteNumRecs(&memWriterAt{}); err == nil {
		t.Error("WriteNumRecs accepted the STREAMING marker as a count")
	}
}

// TestEncodeErrors covers the encoder's refusals.
func TestEncodeErrors(t *testing.T) {
	base := func() *Header {
		h := &Header{Version: Version1, Dims: []Dim{{Name: "x", Size: 2}}}
		h.Vars = []Var{{Name: "v", Type: TypeInt, DimIDs: []int64{0}}}
		if err := h.Layout(); err != nil {
			t.Fatalf("Layout: %v", err)
		}
		return h
	}
	cases := []struct {
		name string
		mut  func(*Header)
	}{
		{"unknown version", func(h *Header) { h.Version = 9 }},
		{"negative numrecs", func(h *Header) { h.NumRecs = -1 }},
		{"cdf1 numrecs too large", func(h *Header) { h.NumRecs = math.MaxUint32 }},
		{"cdf1 begin too large", func(h *Header) { h.Vars[0].Begin = math.MaxInt32 + 1 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := base()
			c.mut(h)
			var buf bytes.Buffer
			if err := h.WriteHeader(&buf); err == nil {
				t.Fatal("WriteHeader accepted an impossible header")
			}
		})
	}
	if err := base().WriteHeader(nil); err == nil {
		t.Error("WriteHeader accepted a nil writer")
	}
}
