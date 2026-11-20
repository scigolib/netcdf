package netcdf_test

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/scigolib/netcdf"
)

// TestSchemaAccessors covers the builder's read accessors.
func TestSchemaAccessors(t *testing.T) {
	s := netcdf.NewSchema(netcdf.FormatClassic64)
	if s.Format() != netcdf.FormatClassic64 {
		t.Errorf("Format = %v", s.Format())
	}
	x := s.AddDimension("x", 4)
	time := s.AddDimension("time", 0)
	if x.Name() != "x" || x.Len() != 4 || x.Unlimited() {
		t.Errorf("x = %q len %d unlimited %v", x.Name(), x.Len(), x.Unlimited())
	}
	if !time.Unlimited() || time.Len() != 0 {
		t.Errorf("time = %q len %d unlimited %v", time.Name(), time.Len(), time.Unlimited())
	}
	if len(s.Dimensions()) != 2 {
		t.Errorf("Dimensions has %d entries", len(s.Dimensions()))
	}
	v := s.AddVariable("v", netcdf.TypeInt, time, x)
	mustAttr(t, v.SetAttr("units", "K"))
	if v.Name() != "v" || v.Type() != netcdf.TypeInt {
		t.Errorf("v = %q type %v", v.Name(), v.Type())
	}
	if len(v.Dims()) != 2 || len(v.Attributes()) != 1 {
		t.Errorf("v has %d dims and %d attributes", len(v.Dims()), len(v.Attributes()))
	}
	if len(s.Variables()) != 1 {
		t.Errorf("Variables has %d entries", len(s.Variables()))
	}
}

// TestSetAttrTypedAndReplace covers the explicit type (ubyte beside char) and
// replacing an attribute in place.
func TestSetAttrTypedAndReplace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.nc")
	s := netcdf.NewSchema(netcdf.FormatClassic64Data)
	x := s.AddDimension("x", 3)
	v := s.AddVariable("v", netcdf.TypeInt, x)
	mustAttr(t, s.SetAttrTyped("raw", netcdf.TypeUByte, []byte{1, 2, 3}))
	mustAttr(t, s.SetAttr("note", "first"))
	mustAttr(t, s.SetAttr("note", "second"))
	w, err := netcdf.Create(path, s)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	wv, ok := w.Variable(v.Name())
	if !ok {
		t.Fatalf("variable %q not found", v.Name())
	}
	if err := wv.Write([]int32{1, 2, 3}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	f, err := netcdf.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = f.Close() }()
	raw, ok := f.Attribute("raw")
	if !ok || raw.Type() != netcdf.TypeUByte {
		t.Fatalf("raw attribute = %v, %v", raw, ok)
	}
	if got := raw.Value(); !reflect.DeepEqual(got, []byte{1, 2, 3}) {
		t.Errorf("raw = %v", got)
	}
	note, ok := f.Attribute("note")
	if !ok || note.String() != "second" {
		t.Errorf("note = %v, want the replaced value", note)
	}
	if n := len(f.Attributes()); n != 2 {
		t.Errorf("file has %d attributes, want 2 after the replacement", n)
	}
}

// TestSetAttrErrors covers the builder's refusals and the sticky error that
// makes an ignored SetAttr failure impossible to miss in Create.
func TestSetAttrErrors(t *testing.T) {
	s := netcdf.NewSchema(netcdf.FormatClassic)
	if err := s.SetAttr("bad", []string{"x"}); !errors.Is(err, netcdf.ErrTypeMismatch) {
		t.Fatalf("SetAttr with []string = %v, want ErrTypeMismatch", err)
	}
	// The error is sticky: Create must fail even though the caller ignored it.
	path := filepath.Join(t.TempDir(), "out.nc")
	if _, err := netcdf.Create(path, s); !errors.Is(err, netcdf.ErrTypeMismatch) {
		t.Fatalf("Create after a failed SetAttr = %v, want the sticky ErrTypeMismatch", err)
	}

	s2 := netcdf.NewSchema(netcdf.FormatClassic)
	x := s2.AddDimension("x", 2)
	v := s2.AddVariable("v", netcdf.TypeInt, x)
	if err := v.SetAttrTyped("units", netcdf.TypeInt, []float32{1}); !errors.Is(err, netcdf.ErrTypeMismatch) {
		t.Fatalf("SetAttrTyped(TypeInt, []float32) = %v, want ErrTypeMismatch", err)
	}
	if err := s2.SetAttrTyped("x", netcdf.Type(99), []byte{1}); !errors.Is(err, netcdf.ErrInvalidSchema) {
		t.Fatalf("SetAttrTyped with an undefined type = %v, want ErrInvalidSchema", err)
	}
}

// TestWriterScalarRoundTrip covers a rank-0 variable.
func TestWriterScalarRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.nc")
	s := netcdf.NewSchema(netcdf.FormatClassic64)
	s.AddVariable("answer", netcdf.TypeInt)
	w, err := netcdf.Create(path, s)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	v, _ := w.Variable("answer")
	if err := v.Write([]int32{42}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	f, err := netcdf.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = f.Close() }()
	rv, _ := f.Variable("answer")
	got := make([]int32, rv.Len())
	if err := rv.Read(&got); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 1 || got[0] != 42 {
		t.Errorf("answer = %v, want [42]", got)
	}
}

// TestWriterSelectionErrors covers the whole-record rule of Write, a zero
// count and a wrong rank.
func TestWriterSelectionErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.nc")
	s := netcdf.NewSchema(netcdf.FormatClassic)
	time := s.AddDimension("time", 0)
	x := s.AddDimension("x", 2)
	s.AddVariable("rec", netcdf.TypeInt, time, x)
	w, err := netcdf.Create(path, s)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer func() { _ = w.Close() }()
	v, _ := w.Variable("rec")
	if err := v.Write([]int32{1, 2, 3}); !errors.Is(err, netcdf.ErrOutOfRange) {
		t.Errorf("partial record Write = %v, want ErrOutOfRange", err)
	}
	if err := v.WriteSlice([]int32{1}, []int64{0}, []int64{0, 2}); !errors.Is(err, netcdf.ErrOutOfRange) {
		t.Errorf("count 0 with one value = %v, want ErrOutOfRange", err)
	}
	if err := v.WriteSlice([]int32{}, []int64{0, 0}, []int64{0, 0}); err != nil {
		t.Errorf("empty selection = %v, want nil", err)
	}
	if got := recs(t, w, "rec"); got != 0 {
		t.Errorf("record count = %d after an empty selection", got)
	}
	if err := v.WriteSlice([]int32{1, 2}, []int64{0}, []int64{1}); !errors.Is(err, netcdf.ErrOutOfRange) {
		t.Errorf("wrong rank = %v, want ErrOutOfRange", err)
	}
}

// TestRejectedWriteDoesNotExtend proves the ordering netCDF-C uses: a
// selection rejected on an inner dimension leaves the record count and the
// file untouched (NCedgeck runs before NCvnrecs).
func TestRejectedWriteDoesNotExtend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.nc")
	s := netcdf.NewSchema(netcdf.FormatClassic)
	time := s.AddDimension("time", 0)
	x := s.AddDimension("x", 2)
	s.AddVariable("temp", netcdf.TypeInt, time, x)
	w, err := netcdf.Create(path, s)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	v, _ := w.Variable("temp")
	err = v.WriteSlice([]int32{5}, []int64{0, 5}, []int64{1, 1})
	if !errors.Is(err, netcdf.ErrOutOfRange) {
		t.Fatalf("out-of-range inner dimension = %v, want ErrOutOfRange", err)
	}
	if got := recs(t, w, "temp"); got != 0 {
		t.Fatalf("a rejected write extended the record count to %d", got)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	f, err := netcdf.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = f.Close() }()
	if numRecs(f) != 0 {
		t.Fatalf("reopened file has %d records, want 0", numRecs(f))
	}
}

// TestZeroCountValidatesAllDimensions proves a zero count is not a license to
// skip the other dimensions, as in netCDF-C's NCedgeck.
func TestZeroCountValidatesAllDimensions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.nc")
	s := netcdf.NewSchema(netcdf.FormatClassic)
	x := s.AddDimension("x", 3)
	y := s.AddDimension("y", 2)
	s.AddVariable("v", netcdf.TypeInt, x, y)
	w, err := netcdf.Create(path, s)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer func() { _ = w.Close() }()
	v, _ := w.Variable("v")
	if err := v.WriteSlice([]int32{}, []int64{0, 100}, []int64{0, 1}); !errors.Is(err, netcdf.ErrOutOfRange) {
		t.Errorf("zero count with a bad inner start = %v, want ErrOutOfRange", err)
	}
	if err := v.WriteSlice([]int32{}, []int64{0, 0}, []int64{0, 0}); err != nil {
		t.Errorf("valid empty selection = %v, want nil", err)
	}
}

// randVar is one generated variable: the values written and re-read.
type randVar struct {
	name   string
	typ    netcdf.Type
	values any
}

// randModel is one generated file.
type randModel struct {
	format    netcdf.Format
	schema    *netcdf.Schema
	vars      []randVar
	wantRecs  int64
	recordVar string // name of one record variable, "" when the model has none
}

// TestRandomRoundTrip is the write→read property over generated models: every
// fixed and record variable comes back with the values written, in all three
// formats.
func TestRandomRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(20261114))
	for iter := 0; iter < 30; iter++ {
		runRoundTrip(t, iter, buildRandomModel(rng))
	}
}

// buildRandomModel generates a small schema with fixed and record variables.
func buildRandomModel(rng *rand.Rand) randModel {
	formats := []netcdf.Format{netcdf.FormatClassic, netcdf.FormatClassic64, netcdf.FormatClassic64Data}
	m := randModel{format: formats[rng.Intn(len(formats))]}
	m.schema = netcdf.NewSchema(m.format)
	ndims := 1 + rng.Intn(2)
	dims := make([]*netcdf.SchemaDim, ndims)
	for i := range dims {
		dims[i] = m.schema.AddDimension(fmt.Sprintf("d%d", i), int64(1+rng.Intn(4)))
	}
	var timeDim *netcdf.SchemaDim
	var records int64
	if rng.Intn(2) == 0 {
		timeDim = m.schema.AddDimension("time", 0)
		records = int64(rng.Intn(4))
	}
	for j := 0; j < 1+rng.Intn(3); j++ {
		name := fmt.Sprintf("v%d", j)
		typ := randomType(rng, m.format)
		vdims, count := randomVarDims(rng, dims[:rng.Intn(ndims+1)], timeDim, records)
		m.schema.AddVariable(name, typ, vdims...)
		m.vars = append(m.vars, randVar{name: name, typ: typ, values: randomValues(rng, typ, count)})
		if timeDim != nil && len(vdims) > 0 && vdims[0] == timeDim {
			m.wantRecs = records
			m.recordVar = name
		}
	}
	return m
}

// randomVarDims picks the dimensions and value count of one variable.
func randomVarDims(rng *rand.Rand, dims []*netcdf.SchemaDim, timeDim *netcdf.SchemaDim, records int64) ([]*netcdf.SchemaDim, int64) {
	var vdims []*netcdf.SchemaDim
	count := int64(1)
	if timeDim != nil && rng.Intn(2) == 0 {
		vdims = append(vdims, timeDim)
		count = records
	}
	for _, d := range dims {
		vdims = append(vdims, d)
		count *= d.Len()
	}
	return vdims, count
}

// runRoundTrip writes one model, closes it, reads it back and compares.
func runRoundTrip(t *testing.T, iter int, m randModel) {
	t.Helper()
	path := filepath.Join(t.TempDir(), fmt.Sprintf("r%d.nc", iter))
	w, err := netcdf.Create(path, m.schema)
	if err != nil {
		t.Fatalf("iteration %d: Create: %v", iter, err)
	}
	for _, wv := range m.vars {
		v, ok := w.Variable(wv.name)
		if !ok {
			t.Fatalf("iteration %d: variable %q not found", iter, wv.name)
		}
		if err := writeTyped(t, v, wv.values); err != nil {
			t.Fatalf("iteration %d: write %q: %v", iter, wv.name, err)
		}
	}
	if m.recordVar != "" {
		if got := recs(t, w, m.recordVar); got != m.wantRecs {
			t.Errorf("iteration %d: record count = %d, want %d", iter, got, m.wantRecs)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("iteration %d: Close: %v", iter, err)
	}
	f, err := netcdf.Open(path)
	if err != nil {
		t.Fatalf("iteration %d: Open: %v", iter, err)
	}
	defer func() { _ = f.Close() }()
	if f.Format() != m.format {
		t.Errorf("iteration %d: format = %v, want %v", iter, f.Format(), m.format)
	}
	for _, wv := range m.vars {
		if got := readTyped(t, f, wv.name, wv.typ); !reflect.DeepEqual(got, wv.values) {
			t.Fatalf("iteration %d: variable %q: read %v, wrote %v", iter, wv.name, got, wv.values)
		}
	}
}

// randomType picks a type legal in the format.
func randomType(rng *rand.Rand, format netcdf.Format) netcdf.Type {
	classic := []netcdf.Type{
		netcdf.TypeByte, netcdf.TypeChar, netcdf.TypeShort,
		netcdf.TypeInt, netcdf.TypeFloat, netcdf.TypeDouble,
	}
	types := classic
	if format == netcdf.FormatClassic64Data {
		types = append(types,
			netcdf.TypeUByte, netcdf.TypeUShort, netcdf.TypeUInt,
			netcdf.TypeInt64, netcdf.TypeUInt64)
	}
	return types[rng.Intn(len(types))]
}

// fillSlice builds count values from next.
func fillSlice[T any](count int64, next func() T) []T {
	s := make([]T, count)
	for i := range s {
		s[i] = next()
	}
	return s
}

// writeTyped writes values through the typed writer method, switching on the
// Go slice type — the dynamic pattern the package docs describe.
func writeTyped(t *testing.T, v *netcdf.VarWriter, values any) error {
	t.Helper()
	switch vals := values.(type) {
	case []int8:
		return v.Write(vals)
	case []byte:
		return v.Write(vals)
	case []int16:
		return v.Write(vals)
	case []uint16:
		return v.Write(vals)
	case []int32:
		return v.Write(vals)
	case []uint32:
		return v.Write(vals)
	case []float32:
		return v.Write(vals)
	case []float64:
		return v.Write(vals)
	case []int64:
		return v.Write(vals)
	case []uint64:
		return v.Write(vals)
	}
	t.Fatalf("unsupported values type %T", values)
	return nil
}

// randomValues builds count values of the Go type matching typ.
func randomValues(rng *rand.Rand, typ netcdf.Type, count int64) any {
	switch typ {
	case netcdf.TypeByte:
		return fillSlice(count, func() int8 { return int8(rng.Intn(255) - 127) })
	case netcdf.TypeChar:
		return fillSlice(count, func() byte { return byte('a' + rng.Intn(26)) })
	case netcdf.TypeShort:
		return fillSlice(count, func() int16 { return int16(rng.Intn(65535) - 32767) })
	case netcdf.TypeInt:
		return fillSlice(count, func() int32 { return rng.Int31() })
	case netcdf.TypeFloat:
		return fillSlice(count, func() float32 { return float32(rng.Intn(400)-200) / 4 })
	case netcdf.TypeDouble:
		return fillSlice(count, func() float64 { return float64(rng.Intn(400)-200) / 8 })
	case netcdf.TypeUByte:
		return fillSlice(count, func() byte { return byte(rng.Intn(256)) })
	case netcdf.TypeUShort:
		return fillSlice(count, func() uint16 { return uint16(rng.Intn(65536)) })
	case netcdf.TypeUInt:
		return fillSlice(count, rng.Uint32)
	case netcdf.TypeInt64:
		return fillSlice(count, func() int64 { return rng.Int63() * int64(1-rng.Intn(2)*2) })
	case netcdf.TypeUInt64:
		return fillSlice(count, rng.Uint64)
	default:
		panic("randomValues: no values for type " + typ.String())
	}
}

// readTyped reads a whole variable back into the Go slice type matching typ.
func readTyped(t *testing.T, f *netcdf.File, name string, typ netcdf.Type) any {
	t.Helper()
	v, ok := f.Variable(name)
	if !ok {
		t.Fatalf("variable %q not found", name)
	}
	n := v.Len()
	switch typ {
	case netcdf.TypeByte:
		s := make([]int8, n)
		return readInto(t, v, s, &s)
	case netcdf.TypeChar, netcdf.TypeUByte:
		s := make([]byte, n)
		return readInto(t, v, s, &s)
	case netcdf.TypeShort:
		s := make([]int16, n)
		return readInto(t, v, s, &s)
	case netcdf.TypeInt:
		s := make([]int32, n)
		return readInto(t, v, s, &s)
	case netcdf.TypeFloat:
		s := make([]float32, n)
		return readInto(t, v, s, &s)
	case netcdf.TypeDouble:
		s := make([]float64, n)
		return readInto(t, v, s, &s)
	case netcdf.TypeUShort:
		s := make([]uint16, n)
		return readInto(t, v, s, &s)
	case netcdf.TypeUInt:
		s := make([]uint32, n)
		return readInto(t, v, s, &s)
	case netcdf.TypeInt64:
		s := make([]int64, n)
		return readInto(t, v, s, &s)
	case netcdf.TypeUInt64:
		s := make([]uint64, n)
		return readInto(t, v, s, &s)
	default:
		t.Fatalf("no Go type for %v", typ)
	}
	return nil
}

// readInto reads into dst and returns the slice for comparison.
func readInto[T any](t *testing.T, v *netcdf.Variable, s []T, dst *[]T) []T {
	t.Helper()
	if err := v.Read(dst); err != nil {
		t.Fatalf("read %q: %v", v.Name(), err)
	}
	return s
}

// TestDimensionLimits pins the per-format dimension limits to netCDF-C's
// NC3_def_dim: X_INT_MAX-3 for CDF-1 and X_UINT_MAX-3 for CDF-2. A dimension
// above them would produce a file the C library refuses to define.
func TestDimensionLimits(t *testing.T) {
	cases := []struct {
		name   string
		format netcdf.Format
		ok     int64
		bad    int64 // 0 means the format has no reachable upper limit
	}{
		{"cdf1", netcdf.FormatClassic, math.MaxInt32 - 3, math.MaxInt32 - 2},
		{"cdf2", netcdf.FormatClassic64, math.MaxUint32 - 3, math.MaxUint32 - 2},
		{"cdf5", netcdf.FormatClassic64Data, math.MaxInt64, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			schema := netcdf.NewSchema(c.format)
			schema.AddDimension("x", c.ok)
			w, err := netcdf.Create(filepath.Join(t.TempDir(), "ok.nc"), schema)
			if err != nil {
				t.Fatalf("Create with the largest accepted dimension %d: %v", c.ok, err)
			}
			if err := w.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
			if c.bad == 0 {
				return
			}
			schema = netcdf.NewSchema(c.format)
			schema.AddDimension("x", c.bad)
			_, err = netcdf.Create(filepath.Join(t.TempDir(), "bad.nc"), schema)
			if !errors.Is(err, netcdf.ErrInvalidSchema) {
				t.Fatalf("Create with dimension %d: %v, want ErrInvalidSchema", c.bad, err)
			}
		})
	}
}
