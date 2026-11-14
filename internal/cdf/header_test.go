package cdf

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

// The tests build classic files themselves, from the specification's grammar,
// independently of the parser: the builder below is a second, test-only
// implementation of the wire format. The two published files from the
// specification ("empty" and "tiny") are transcribed byte for byte.

// Published examples from the NetCDF Classic Format Specification.

var specEmptyCDF1 = []byte{
	'C', 'D', 'F', 1,
	0, 0, 0, 0, // numrecs
	0, 0, 0, 0, 0, 0, 0, 0, // dim_list ABSENT
	0, 0, 0, 0, 0, 0, 0, 0, // gatt_list ABSENT
	0, 0, 0, 0, 0, 0, 0, 0, // var_list ABSENT
}

var specTinyCDF1 = []byte{
	'C', 'D', 'F', 1,
	0, 0, 0, 0, // numrecs
	0, 0, 0, 10, 0, 0, 0, 1, // NC_DIMENSION, 1
	0, 0, 0, 3, 'd', 'i', 'm', 0, // name "dim", padded
	0, 0, 0, 5, // size 5
	0, 0, 0, 0, 0, 0, 0, 0, // gatt_list ABSENT
	0, 0, 0, 11, 0, 0, 0, 1, // NC_VARIABLE, 1
	0, 0, 0, 2, 'v', 'x', 0, 0, // name "vx", padded
	0, 0, 0, 1, // ndims
	0, 0, 0, 0, // dimid 0
	0, 0, 0, 0, 0, 0, 0, 0, // vatt_list ABSENT
	0, 0, 0, 3, // NC_SHORT
	0, 0, 0, 12, // vsize 12
	0, 0, 0, 80, // begin 80
	0, 3, 0, 1, 0, 4, 0, 1, 0, 5, 0x80, 0x01, // values 3,1,4,1,5 and fill
}

// builder encodes classic-format bytes.
type builder struct {
	b []byte
	v Version
}

func (w *builder) bytes(p []byte) { w.b = append(w.b, p...) }

func (w *builder) u32(x uint32) {
	w.b = append(w.b, byte(x>>24), byte(x>>16), byte(x>>8), byte(x))
}

func (w *builder) u64(x uint64) {
	w.b = append(w.b,
		byte(x>>56), byte(x>>48), byte(x>>40), byte(x>>32),
		byte(x>>24), byte(x>>16), byte(x>>8), byte(x))
}

// st writes a size_t-shaped integer: 32 bits, or 64 in CDF-5.
func (w *builder) st(x int64) {
	if w.v == Version5 {
		w.u64(uint64(x))
		return
	}
	w.u32(uint32(x))
}

// ofs writes an offset: 32 bits in CDF-1, 64 in CDF-2 and CDF-5.
func (w *builder) ofs(x int64) {
	if w.v == Version1 {
		w.u32(uint32(x))
		return
	}
	w.u64(uint64(x))
}

func (w *builder) pad4() {
	for len(w.b)%4 != 0 {
		w.b = append(w.b, 0)
	}
}

func (w *builder) name(s string) {
	w.st(int64(len(s)))
	w.bytes([]byte(s))
	w.pad4()
}

// fixture describes a file to build.
type fixture struct {
	v       Version
	numrecs int64
	dims    []fxDim
	gattrs  []fxAttr
	vars    []fxVar

	// Overrides for malformed-input tests.
	dimTag, gattTag, varTag       *uint32
	dimCount, gattCount, varCount *int64
}

type fxDim struct {
	name string
	size int64
}

type fxAttr struct {
	name string
	typ  Type
	data []byte // external representation
}

type fxVar struct {
	name   string
	dimIDs []int64
	attrs  []fxAttr
	typ    Type
	begin  *int64 // override; nil computes the layout
	vsize  *int64 // override; nil writes roundUp4(data size)
}

func ptrTo[T any](v T) *T { return &v }

func testRoundUp4(n int64) int64 { return (n + 3) &^ 3 }

// dataSize returns the unpadded size of one slab of values, or 0 when the
// dimensions of a deliberately-malformed fixture cannot be resolved.
func (fx *fixture) dataSize(v fxVar) int64 {
	n := int64(1)
	for i, id := range v.dimIDs {
		if id < 0 || int(id) >= len(fx.dims) {
			return 0
		}
		if i == 0 && fx.dims[id].size == 0 {
			continue // record dimension
		}
		n *= fx.dims[id].size
	}
	return n * int64(v.typ.Size())
}

// orDefault returns *over when it is set, def otherwise.
func orDefault[T any](over *T, def T) T {
	if over != nil {
		return *over
	}
	return def
}

func (fx *fixture) encode(begins []int64) []byte {
	w := &builder{v: fx.v}
	w.bytes([]byte{'C', 'D', 'F', byte(fx.v)})
	w.st(fx.numrecs)
	fx.writeDims(w)
	fx.writeAttrs(w, fx.gattrs, fx.gattTag, fx.gattCount)
	fx.writeVars(w, begins)
	return w.b
}

func (fx *fixture) writeDims(w *builder) {
	w.u32(orDefault(fx.dimTag, uint32(10))) // NC_DIMENSION
	w.st(orDefault(fx.dimCount, int64(len(fx.dims))))
	for _, d := range fx.dims {
		w.name(d.name)
		w.st(d.size)
	}
}

// writeAttrs writes one complete attribute list: tag, count, entries.
func (fx *fixture) writeAttrs(w *builder, as []fxAttr, tag *uint32, cnt *int64) {
	if as == nil && tag == nil && cnt == nil {
		w.u32(0) // ABSENT
		w.st(0)
		return
	}
	w.u32(orDefault(tag, uint32(12))) // NC_ATTRIBUTE
	w.st(orDefault(cnt, int64(len(as))))
	for _, a := range as {
		w.name(a.name)
		w.u32(uint32(a.typ))
		w.st(int64(len(a.data)) / int64(a.typ.Size()))
		w.bytes(a.data)
		w.pad4()
	}
}

func (fx *fixture) writeVars(w *builder, begins []int64) {
	w.u32(orDefault(fx.varTag, uint32(11))) // NC_VARIABLE
	w.st(orDefault(fx.varCount, int64(len(fx.vars))))
	for i, v := range fx.vars {
		fx.writeVar(w, v, orDefault(v.begin, begins[i]))
	}
}

func (fx *fixture) writeVar(w *builder, v fxVar, begin int64) {
	w.name(v.name)
	w.st(int64(len(v.dimIDs)))
	for _, id := range v.dimIDs {
		if fx.v == Version5 {
			w.u64(uint64(id))
		} else {
			w.u32(uint32(id))
		}
	}
	fx.writeAttrs(w, v.attrs, nil, nil)
	w.u32(uint32(v.typ))
	w.st(orDefault(v.vsize, testRoundUp4(fx.dataSize(v))))
	w.ofs(begin)
}

// header returns the header with begins laid out as netCDF-C lays them out:
// the fixed-size variables first, then the record variables. Like netCDF-C's
// NC_begins it assumes every fixed variable is defined before the first record
// variable; a fixture that breaks that order needs explicit begins.
func (fx *fixture) header() []byte {
	hdr := fx.encode(make([]int64, len(fx.vars)))
	begins := make([]int64, len(fx.vars))
	cur := int64(len(hdr))
	for i, v := range fx.vars {
		begins[i] = cur
		cur += testRoundUp4(fx.dataSize(v))
	}
	return fx.encode(begins)
}

// file returns the header followed by data.
func (fx *fixture) file(data []byte) []byte {
	return append(fx.header(), data...)
}

// External encoders for data sections.

func be16(vals ...int16) []byte {
	b := make([]byte, 2*len(vals))
	for i, v := range vals {
		binary.BigEndian.PutUint16(b[2*i:], uint16(v))
	}
	return b
}

func be32(vals ...int32) []byte {
	b := make([]byte, 4*len(vals))
	for i, v := range vals {
		binary.BigEndian.PutUint32(b[4*i:], uint32(v))
	}
	return b
}

func be64(vals ...int64) []byte {
	b := make([]byte, 8*len(vals))
	for i, v := range vals {
		binary.BigEndian.PutUint64(b[8*i:], uint64(v))
	}
	return b
}

func cat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func mustHeader(t *testing.T, data []byte) *Header {
	t.Helper()
	h, err := ReadHeader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	return h
}

// runs collects the contiguous runs WalkVara emits.
func runs(t *testing.T, h *Header, v *Var, start, count []int64) [][2]int64 {
	t.Helper()
	var out [][2]int64
	err := h.WalkVara(v, start, count, func(off, n int64) error {
		out = append(out, [2]int64{off, n})
		return nil
	})
	if err != nil {
		t.Fatalf("WalkVara(%v, %v): %v", start, count, err)
	}
	return out
}

func TestTypeSize(t *testing.T) {
	want := map[Type]int{
		TypeByte: 1, TypeChar: 1, TypeShort: 2, TypeInt: 4, TypeFloat: 4,
		TypeDouble: 8, TypeUByte: 1, TypeUShort: 2, TypeUInt: 4,
		TypeInt64: 8, TypeUInt64: 8,
	}
	for typ, size := range want {
		if got := typ.Size(); got != size {
			t.Errorf("Type(%d).Size() = %d, want %d", typ, got, size)
		}
	}
	if got := Type(0).Size(); got != 0 {
		t.Errorf("Type(0).Size() = %d, want 0", got)
	}
	if Type(12).validFor(Version5) {
		t.Error("NC_STRING accepted by a classic header")
	}
	if TypeUByte.validFor(Version1) || !TypeUByte.validFor(Version5) {
		t.Error("CDF-5-only type not gated by version")
	}
}

func TestReadHeaderSpecEmpty(t *testing.T) {
	h := mustHeader(t, specEmptyCDF1)
	if h.Version != Version1 {
		t.Errorf("Version = %v, want 1", h.Version)
	}
	if h.NumRecs != 0 || len(h.Dims) != 0 || len(h.Attrs) != 0 || len(h.Vars) != 0 {
		t.Errorf("unexpected content: %+v", h)
	}
	if h.HeaderSize != 32 {
		t.Errorf("HeaderSize = %d, want 32", h.HeaderSize)
	}
	if h.RecSize != 0 {
		t.Errorf("RecSize = %d, want 0", h.RecSize)
	}
}

func TestReadHeaderSpecTiny(t *testing.T) {
	h := mustHeader(t, specTinyCDF1)
	if h.Version != Version1 {
		t.Fatalf("Version = %v", h.Version)
	}
	if len(h.Dims) != 1 || h.Dims[0].Name != "dim" || h.Dims[0].Size != 5 {
		t.Fatalf("Dims = %+v", h.Dims)
	}
	if h.HeaderSize != 80 {
		t.Errorf("HeaderSize = %d, want 80", h.HeaderSize)
	}
	if len(h.Vars) != 1 {
		t.Fatalf("Vars = %+v", h.Vars)
	}
	v := &h.Vars[0]
	if v.Name != "vx" || v.Type != TypeShort || v.Begin != 80 || v.DataSize != 10 {
		t.Errorf("Var = %+v", v)
	}
	if v.Record || len(v.Shape) != 1 || v.Shape[0] != 5 {
		t.Errorf("shape = %v record=%v", v.Shape, v.Record)
	}
	got := runs(t, h, v, []int64{0}, []int64{5})
	if len(got) != 1 || got[0] != [2]int64{80, 5} {
		t.Errorf("runs = %v", got)
	}
	raw := specTinyCDF1[80:90]
	want := []int16{3, 1, 4, 1, 5}
	for i, wv := range want {
		if gv := int16(binary.BigEndian.Uint16(raw[2*i:])); gv != wv {
			t.Errorf("value[%d] = %d, want %d", i, gv, wv)
		}
	}
}

func TestReadHeaderCDF2(t *testing.T) {
	fx := &fixture{
		v:       Version2,
		numrecs: 0,
		dims:    []fxDim{{"d", 4}},
		gattrs:  []fxAttr{{"title", TypeChar, []byte("hi!\x00")}},
		vars:    []fxVar{{name: "v", dimIDs: []int64{0}, typ: TypeInt}},
	}
	data := be32(1, 2, 3, 4)
	f := fx.file(data)
	h := mustHeader(t, f)
	if h.Version != Version2 {
		t.Fatalf("Version = %v", h.Version)
	}
	if len(h.Attrs) != 1 || h.Attrs[0].Name != "title" || string(h.Attrs[0].Data) != "hi!\x00" {
		t.Fatalf("Attrs = %+v", h.Attrs)
	}
	v := &h.Vars[0]
	if v.Begin != h.HeaderSize {
		t.Errorf("Begin = %d, HeaderSize = %d", v.Begin, h.HeaderSize)
	}
	got := runs(t, h, v, []int64{0}, []int64{4})
	if len(got) != 1 || got[0][1] != 4 {
		t.Fatalf("runs = %v", got)
	}
	raw := f[got[0][0]:]
	for i := 0; i < 4; i++ {
		if gv := int32(binary.BigEndian.Uint32(raw[4*i:])); gv != int32(i+1) {
			t.Errorf("value[%d] = %d, want %d", i, gv, i+1)
		}
	}
}

func TestReadHeaderCDF5(t *testing.T) {
	fx := &fixture{
		v:       Version5,
		numrecs: 0,
		dims:    []fxDim{{"a", 2}, {"b", 3}},
		vars: []fxVar{
			{name: "i64", dimIDs: []int64{0}, typ: TypeInt64},
			{name: "u64", dimIDs: []int64{0}, typ: TypeUInt64},
			{name: "u16", dimIDs: []int64{1}, typ: TypeUShort},
			{name: "u8", dimIDs: []int64{0}, typ: TypeUByte},
		},
	}
	// The u8 layer: u16's three values occupy six bytes, padded to eight, so
	// the data section carries two pad bytes before u8 (vsize is padded).
	data := cat(
		be64(1, 2),
		be64(3, 4),
		[]byte{0, 5, 0, 6, 0, 7, 0, 0},
		[]byte{8, 9},
	)
	f := fx.file(data)
	h := mustHeader(t, f)
	if h.Version != Version5 {
		t.Fatalf("Version = %v", h.Version)
	}
	wantTypes := []Type{TypeInt64, TypeUInt64, TypeUShort, TypeUByte}
	for i, want := range wantTypes {
		if h.Vars[i].Type != want {
			t.Errorf("Vars[%d].Type = %d, want %d", i, h.Vars[i].Type, want)
		}
	}
	if v := &h.Vars[2]; v.Shape[0] != 3 || v.DataSize != 6 {
		t.Errorf("u16 var = %+v", v)
	}
	// u16's three values occupy six bytes, padded to eight; u8 starts after
	// the padding, and reading both proves the layout and the begins.
	u16 := &h.Vars[2]
	u16Run := runs(t, h, u16, []int64{0}, []int64{3})
	if len(u16Run) != 1 || u16Run[0] != [2]int64{u16.Begin, 3} {
		t.Fatalf("u16 runs = %v", u16Run)
	}
	for i, want := range []uint16{5, 6, 7} {
		if got := binary.BigEndian.Uint16(f[u16Run[0][0]+int64(2*i):]); got != want {
			t.Errorf("u16 value %d = %d, want %d", i, got, want)
		}
	}
	u8 := &h.Vars[3]
	if u8.Begin != u16.Begin+8 {
		t.Errorf("u8.Begin = %d, want %d (after the padded u16 slab)", u8.Begin, u16.Begin+8)
	}
	u8Run := runs(t, h, u8, []int64{0}, []int64{2})
	if len(u8Run) != 1 || u8Run[0] != [2]int64{u8.Begin, 2} {
		t.Fatalf("u8 runs = %v", u8Run)
	}
	if f[u8Run[0][0]] != 8 || f[u8Run[0][0]+1] != 9 {
		t.Errorf("u8 values = %v, %v; want 8, 9", f[u8Run[0][0]], f[u8Run[0][0]+1])
	}
	if v := &h.Vars[3]; v.Shape[0] != 2 || v.DataSize != 2 {
		t.Errorf("u8 var = %+v", v)
	}
}

func TestRecordVariables(t *testing.T) {
	fx := &fixture{
		v:       Version1,
		numrecs: 2,
		dims:    []fxDim{{"time", 0}, {"x", 2}, {"y", 3}},
		vars: []fxVar{
			{name: "f", dimIDs: []int64{1, 2}, typ: TypeDouble}, // fixed 2x3
			{name: "r1", dimIDs: []int64{0, 1}, typ: TypeShort}, // record 2 per record
			{name: "r2", dimIDs: []int64{0}, typ: TypeInt},      // record 1 per record
		},
	}
	// fixed: 6 doubles = 48 bytes; per record: r1 4 bytes + r2 4 bytes = 8.
	data := cat(
		be64(1, 2, 3, 4, 5, 6),
		be16(10, 11), be32(100), // record 0
		be16(12, 13), be32(200), // record 1
	)
	f := fx.file(data)
	h := mustHeader(t, f)
	if h.RecSize != 8 {
		t.Fatalf("RecSize = %d, want 8", h.RecSize)
	}
	r1, r2 := &h.Vars[1], &h.Vars[2]
	if !r1.Record || !r2.Record {
		t.Fatalf("record flags: %+v %+v", r1, r2)
	}
	if r1.DataSize != 4 || r1.SlabSize != 4 || r2.DataSize != 4 {
		t.Fatalf("slab sizes: r1=%+v r2=%+v", r1, r2)
	}
	if r2.Begin != r1.Begin+4 {
		t.Errorf("r2.Begin = %d, want %d", r2.Begin, r1.Begin+4)
	}
	// Record 1 of r1 sits at r1.Begin + RecSize.
	got := runs(t, h, r1, []int64{1, 0}, []int64{1, 2})
	if len(got) != 1 || got[0] != [2]int64{r1.Begin + h.RecSize, 2} {
		t.Fatalf("runs = %v", got)
	}
	off := int(got[0][0])
	if v := int16(binary.BigEndian.Uint16(f[off:])); v != 12 {
		t.Errorf("r1[1,0] = %d, want 12", v)
	}
	if v := int16(binary.BigEndian.Uint16(f[off+2:])); v != 13 {
		t.Errorf("r1[1,1] = %d, want 13", v)
	}
	// Record 0 of r2.
	got = runs(t, h, r2, []int64{0}, []int64{1})
	if len(got) != 1 || got[0] != [2]int64{r2.Begin, 1} {
		t.Fatalf("runs = %v", got)
	}
	if v := int32(binary.BigEndian.Uint32(f[got[0][0]:])); v != 100 {
		t.Errorf("r2[0] = %d, want 100", v)
	}
}

func TestSingleRecordVariableNoPadding(t *testing.T) {
	fx := &fixture{
		v:       Version1,
		numrecs: 3,
		dims:    []fxDim{{"time", 0}},
		vars:    []fxVar{{name: "c", dimIDs: []int64{0}, typ: TypeByte}},
	}
	f := fx.file([]byte{'a', 'b', 'c'})
	h := mustHeader(t, f)
	if h.RecSize != 1 {
		t.Fatalf("RecSize = %d, want 1 (no padding for a single record variable)", h.RecSize)
	}
	v := &h.Vars[0]
	got := runs(t, h, v, []int64{2}, []int64{1})
	if len(got) != 1 || got[0] != [2]int64{v.Begin + 2, 1} {
		t.Fatalf("runs = %v", got)
	}
	if f[got[0][0]] != 'c' {
		t.Errorf("record 2 = %q, want 'c'", f[got[0][0]])
	}
}

func TestStreamingNumRecs(t *testing.T) {
	fx := &fixture{
		v:       Version1,
		numrecs: 0xFFFFFFFF, // STREAMING
		dims:    []fxDim{{"time", 0}},
		vars:    []fxVar{{name: "b", dimIDs: []int64{0}, typ: TypeByte}},
	}
	h := mustHeader(t, fx.file([]byte{1, 2, 3, 4, 5}))
	if h.NumRecs != 5 {
		t.Errorf("NumRecs = %d, want 5 computed from the file size", h.NumRecs)
	}
}

func TestTruncatedHeaderNeverPanics(t *testing.T) {
	fx := &fixture{
		v:       Version1,
		numrecs: 1,
		dims:    []fxDim{{"d", 2}},
		gattrs:  []fxAttr{{"units", TypeChar, []byte("m\x00")}},
		vars:    []fxVar{{name: "v", dimIDs: []int64{0}, typ: TypeInt}},
	}
	good := fx.file(be32(7, 8))
	headerLen := len(fx.header())
	for n := 0; n < len(good); n++ {
		h, err := ReadHeader(bytes.NewReader(good[:n]), int64(n))
		if n < headerLen {
			if err == nil {
				t.Fatalf("truncated at %d: no error, header %+v", n, h)
			}
			continue
		}
		if err != nil {
			t.Fatalf("truncated at %d (header is %d bytes): %v", n, headerLen, err)
		}
	}
}

func TestCorruptHeaders(t *testing.T) {
	base := func() *fixture {
		return &fixture{
			v:       Version1,
			numrecs: 1,
			dims:    []fxDim{{"d", 2}},
			vars:    []fxVar{{name: "v", dimIDs: []int64{0}, typ: TypeInt}},
		}
	}
	withData := func(fx *fixture) []byte { return fx.file(be32(1, 2)) }

	cases := []struct {
		name string
		file []byte
	}{
		{"bad magic", cat([]byte{'X', 'D', 'F', 1}, specEmptyCDF1[4:])},
		{"version 3", cat([]byte{'C', 'D', 'F', 3}, specEmptyCDF1[4:])},
		{"magic only", specEmptyCDF1[:4]},
		{"wrong dim tag", func() []byte { fx := base(); fx.dimTag = ptrTo(uint32(99)); return withData(fx) }()},
		{"huge dim count", func() []byte { fx := base(); fx.dimCount = ptrTo(int64(1 << 30)); return withData(fx) }()},
		{"dimid out of range", func() []byte {
			fx := base()
			fx.vars = []fxVar{{name: "v", dimIDs: []int64{9}, typ: TypeInt, begin: ptrTo(int64(200)), vsize: ptrTo(int64(8))}}
			return withData(fx)
		}()},
		{"unlimited dim not first", func() []byte {
			fx := base()
			fx.dims = []fxDim{{"a", 2}, {"z", 0}}
			fx.vars = []fxVar{{name: "v", dimIDs: []int64{0, 1}, typ: TypeInt}}
			return withData(fx)
		}()},
		{"two unlimited dims", func() []byte {
			fx := base()
			fx.dims = []fxDim{{"z1", 0}, {"z2", 0}}
			fx.vars = []fxVar{
				{name: "a", dimIDs: []int64{0}, typ: TypeInt},
				{name: "b", dimIDs: []int64{1}, typ: TypeInt},
			}
			return withData(fx)
		}()},
		{"CDF-5 type in CDF-1", func() []byte {
			fx := base()
			fx.vars[0].typ = TypeUByte
			return withData(fx)
		}()},
		{"NC_STRING type", func() []byte {
			fx := base()
			fx.vars[0].typ = Type(12)
			return withData(fx)
		}()},
		{"rank above limit", func() []byte {
			fx := base()
			ids := make([]int64, 1025)
			fx.vars = []fxVar{{name: "v", dimIDs: ids, typ: TypeInt}}
			return withData(fx)
		}()},
		{"CDF-1 begin beyond the signed range", func() []byte {
			fx := base()
			fx.vars[0].begin = ptrTo(int64(4294967295))
			return withData(fx)
		}()},
		{"begin zero", func() []byte {
			fx := base()
			fx.vars[0].begin = ptrTo(int64(0))
			return withData(fx)
		}()},
		{"begin inside header", func() []byte {
			fx := base()
			fx.vars[0].begin = ptrTo(int64(4))
			return withData(fx)
		}()},
		{"fixed variables overlap", func() []byte {
			fx := base()
			fx.vars = []fxVar{
				{name: "a", dimIDs: []int64{0}, typ: TypeInt, begin: ptrTo(int64(200)), vsize: ptrTo(int64(8))},
				{name: "b", dimIDs: []int64{0}, typ: TypeInt, begin: ptrTo(int64(200)), vsize: ptrTo(int64(8))},
			}
			return withData(fx)
		}()},
		{"record section before fixed data", func() []byte {
			fx := base()
			fx.dims = []fxDim{{"z", 0}, {"d", 2}}
			fx.vars = []fxVar{
				{name: "f", dimIDs: []int64{1}, typ: TypeInt, begin: ptrTo(int64(300)), vsize: ptrTo(int64(8))},
				{name: "r", dimIDs: []int64{0}, typ: TypeInt, begin: ptrTo(int64(304)), vsize: ptrTo(int64(4))},
			}
			return withData(fx)
		}()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, err := ReadHeader(bytes.NewReader(tc.file), int64(len(tc.file)))
			if err == nil {
				t.Fatalf("no error; header = %+v", h)
			}
			if !errors.Is(err, ErrCorrupt) && !errors.Is(err, ErrNotNetCDF) {
				t.Fatalf("error %v does not wrap ErrCorrupt or ErrNotNetCDF", err)
			}
		})
	}
}

func TestWalkVaraFixed(t *testing.T) {
	fx := &fixture{
		v:      Version1,
		dims:   []fxDim{{"x", 4}, {"y", 3}},
		vars:   []fxVar{{name: "a", dimIDs: []int64{0, 1}, typ: TypeFloat}},
		dimTag: nil,
	}
	vals := make([]byte, 48)
	for i := 0; i < 12; i++ {
		binary.BigEndian.PutUint32(vals[4*i:], uint32(i))
	}
	f := fx.file(vals)
	h := mustHeader(t, f)
	v := &h.Vars[0]

	// Whole variable: one contiguous run.
	got := runs(t, h, v, []int64{0, 0}, []int64{4, 3})
	if len(got) != 1 || got[0] != [2]int64{v.Begin, 12} {
		t.Fatalf("full runs = %v", got)
	}
	// Rows x=1..2, both y columns: the innermost dimension is contiguous even
	// when only part of it is selected, so this is one run of 2 per x.
	got = runs(t, h, v, []int64{1, 0}, []int64{2, 2})
	if len(got) != 2 {
		t.Fatalf("runs = %v", got)
	}
	for i, r := range got {
		wantOff := v.Begin + int64((1+i)*3)*4
		if r != [2]int64{wantOff, 2} {
			t.Errorf("run %d = %v, want {%d 2}", i, r, wantOff)
		}
	}
	// One full column: x=2, all y.
	got = runs(t, h, v, []int64{2, 0}, []int64{1, 3})
	if len(got) != 1 || got[0] != [2]int64{v.Begin + 6*4, 3} {
		t.Fatalf("column runs = %v", got)
	}
	// Empty selection: no runs.
	if got = runs(t, h, v, []int64{0, 0}, []int64{0, 3}); len(got) != 0 {
		t.Fatalf("empty selection runs = %v", got)
	}
}

// TestWalkVaraCoalescing pins the rule the 2026-11-14 review found broken:
// fully selected outer dimensions with a partial innermost selection must not
// merge into one run, and the innermost start must be applied.
func TestWalkVaraCoalescing(t *testing.T) {
	fx := &fixture{
		v:    Version1,
		dims: []fxDim{{"a", 4}, {"b", 3}, {"c", 4}},
		vars: []fxVar{{name: "v", dimIDs: []int64{0, 1, 2}, typ: TypeInt}},
	}
	data := make([]byte, 4*3*4*4)
	for i := 0; i < 48; i++ {
		binary.BigEndian.PutUint32(data[4*i:], uint32(i))
	}
	h := mustHeader(t, fx.file(data))
	v := &h.Vars[0]

	// Outer dimensions fully selected, the innermost picked from the middle:
	// one run of two per (a, b), not one bogus run of twelve.
	got := runs(t, h, v, []int64{1, 0, 2}, []int64{2, 3, 2})
	if len(got) != 6 {
		t.Fatalf("runs = %v, want 6", got)
	}
	for i, r := range got {
		a := int64(1) + int64(i/3)
		b := int64(i % 3)
		wantOff := v.Begin + ((a*3+b)*4+2)*4
		if r != [2]int64{wantOff, 2} {
			t.Errorf("run %d = %v, want {%d 2}", i, r, wantOff)
		}
	}

	// The whole variable is still one run.
	if got = runs(t, h, v, []int64{0, 0, 0}, []int64{4, 3, 4}); len(got) != 1 || got[0] != [2]int64{v.Begin, 48} {
		t.Errorf("full runs = %v", got)
	}

	// The same combination in two dimensions, the 2-D form of the review's
	// repro: a [4,3] variable, every row selected, columns 1..2.
	fx2 := &fixture{
		v:    Version1,
		dims: []fxDim{{"x", 4}, {"y", 3}},
		vars: []fxVar{{name: "w", dimIDs: []int64{0, 1}, typ: TypeInt}},
	}
	h2 := mustHeader(t, fx2.file(make([]byte, 48)))
	w := &h2.Vars[0]
	got2 := runs(t, h2, w, []int64{0, 1}, []int64{4, 2})
	if len(got2) != 4 {
		t.Fatalf("2-D runs = %v, want 4", got2)
	}
	for i, r := range got2 {
		wantOff := w.Begin + int64(i*3+1)*4
		if r != [2]int64{wantOff, 2} {
			t.Errorf("2-D run %d = %v, want {%d 2}", i, r, wantOff)
		}
	}
}

func TestContiguousFrom(t *testing.T) {
	v := &Var{Shape: []int64{4, 3, 4}}
	cases := []struct {
		start, count []int64
		from, want   int
	}{
		{[]int64{0, 0, 0}, []int64{4, 3, 4}, 0, 0}, // whole variable
		{[]int64{0, 0, 0}, []int64{4, 3, 1}, 0, 2}, // partial innermost only
		{[]int64{0, 0, 2}, []int64{4, 3, 2}, 0, 2}, // full outers, innermost from the middle
		{[]int64{0, 0, 0}, []int64{4, 2, 4}, 0, 2}, // partial middle: runs per (a, b)
		{[]int64{1, 0, 0}, []int64{2, 3, 4}, 0, 1}, // inner two full: a run per outer value
		{[]int64{0, 0, 0}, []int64{1, 3, 4}, 1, 1}, // record-variable slab from dimension 1
		{[]int64{0, 0, 0}, []int64{1, 3, 4}, 3, 3}, // no slab dimensions at all
	}
	for _, tc := range cases {
		if got := contiguousFrom(v, tc.start, tc.count, tc.from); got != tc.want {
			t.Errorf("contiguousFrom(%v, %v, from %d) = %d, want %d",
				tc.start, tc.count, tc.from, got, tc.want)
		}
	}
}

func TestWalkVaraBounds(t *testing.T) {
	fx := &fixture{
		v:    Version1,
		dims: []fxDim{{"d", 4}},
		vars: []fxVar{{name: "v", dimIDs: []int64{0}, typ: TypeInt}},
	}
	h := mustHeader(t, fx.file(be32(1, 2, 3, 4)))
	v := &h.Vars[0]
	cases := []struct{ start, count []int64 }{
		{[]int64{0}, []int64{5}},       // count beyond the shape
		{[]int64{3}, []int64{2}},       // start+count beyond
		{[]int64{-1}, []int64{1}},      // negative start
		{[]int64{0}, []int64{-1}},      // negative count
		{[]int64{}, []int64{1}},        // rank mismatch
		{[]int64{0, 0}, []int64{1, 1}}, // rank mismatch
	}
	for _, tc := range cases {
		err := h.WalkVara(v, tc.start, tc.count, func(_, _ int64) error { return nil })
		if !errors.Is(err, ErrOutOfRange) {
			t.Errorf("WalkVara(%v, %v) = %v, want ErrOutOfRange", tc.start, tc.count, err)
		}
	}
}

// maxFuzzRecords bounds the record walk in FuzzReadHeader: a crafted header
// with numrecs near 2^32 must not make one input run for half a minute.
const maxFuzzRecords = 4

func FuzzReadHeader(f *testing.F) {
	for _, seed := range [][]byte{specEmptyCDF1, specTinyCDF1} {
		f.Add(seed)
	}
	valid := (&fixture{
		v:       Version5,
		numrecs: 2,
		dims:    []fxDim{{"time", 0}, {"d", 2}},
		gattrs:  []fxAttr{{"units", TypeChar, []byte("m\x00")}},
		vars: []fxVar{
			{name: "f", dimIDs: []int64{1}, typ: TypeDouble},
			{name: "r", dimIDs: []int64{0, 1}, typ: TypeInt64},
		},
	}).file(cat(be64(1, 2), be64(3, 4), be64(5, 6)))
	f.Add(valid)

	f.Fuzz(func(t *testing.T, b []byte) {
		h, err := ReadHeader(bytes.NewReader(b), int64(len(b)))
		if err != nil {
			return
		}
		for i := range h.Vars {
			v := &h.Vars[i]
			start := make([]int64, len(v.Shape))
			count := append([]int64(nil), v.Shape...)
			if v.Record {
				// The walk is bounded so a crafted numrecs cannot make one
				// input emit billions of runs; the geometry is what this
				// target proves, not the record count.
				count[0] = h.NumRecs
				if count[0] > maxFuzzRecords {
					count[0] = maxFuzzRecords
				}
			}
			if walkErr := h.WalkVara(v, start, count, func(off, n int64) error {
				if n < 0 || off < 0 {
					t.Fatalf("negative run (%d, %d)", off, n)
				}
				return nil
			}); walkErr != nil {
				t.Fatalf("WalkVara on parsed header: %v", walkErr)
			}
		}
	})
}
