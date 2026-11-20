package netcdf_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/scigolib/netcdf"
)

// recs returns the writer's current record count through a record variable's
// shape (the record count is per dimension now, not a separate public value).
func recs(t *testing.T, w *netcdf.Writer, name string) int64 {
	t.Helper()
	v, ok := w.Variable(name)
	if !ok {
		t.Fatalf("variable %q not found", name)
	}
	shape := v.Shape()
	if len(shape) == 0 {
		t.Fatalf("variable %q has no dimensions", name)
	}
	return shape[0]
}

// TestWriterRoundTrip reads back what we wrote through the public reader: the
// metadata, the fixed variable, the record variables and the record count.
func TestWriterRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.nc")
	w, err := netcdf.Create(path, buildLayout(t, netcdf.FormatClassic64Data))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	writeLayout(t, w)
	if got := recs(t, w, "temp"); got != 5 {
		t.Errorf("record count = %d, want 5", got)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	f, err := netcdf.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = f.Close() }()
	if f.Format() != netcdf.FormatClassic64Data {
		t.Errorf("Format = %v, want CDF-5", f.Format())
	}
	if numRecs(f) != 5 {
		t.Errorf("record count = %d, want 5", numRecs(f))
	}
	cube, ok := f.Variable("cube")
	if !ok {
		t.Fatal("cube not found")
	}
	got := make([]float32, cube.Len())
	if err := cube.Read(&got); err != nil {
		t.Fatalf("read cube: %v", err)
	}
	for i := range got {
		if got[i] != float32(i) {
			t.Fatalf("cube[%d] = %v, want %d", i, got[i], i)
		}
	}
	v, _ := f.Variable("temp")
	temp := make([]int32, v.Len())
	if err := v.Read(&temp); err != nil {
		t.Fatalf("read temp: %v", err)
	}
	want := []int32{10, 11, 12, 20, 21, 22, 30, 31, 32, 40, 41, 42, 50, 51, 52}
	if len(temp) != len(want) {
		t.Fatalf("temp has %d values, want %d", len(temp), len(want))
	}
	for i := range want {
		if temp[i] != want[i] {
			t.Fatalf("temp[%d] = %d, want %d", i, temp[i], want[i])
		}
	}
}

// TestWriterRecordExtension proves NCfillrecord's semantics: a write past the
// current count fills every record in between with the fill value, for every
// record variable, before the written values land.
func TestWriterRecordExtension(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.nc")
	s := netcdf.NewSchema(netcdf.FormatClassic)
	time := s.AddDimension("time", 0)
	x := s.AddDimension("x", 2)
	s.AddVariable("temp", netcdf.TypeInt, time, x)
	s.AddVariable("flag", netcdf.TypeShort, time)
	w, err := netcdf.Create(path, s)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Write record 2 only: records 0 and 1 appear as fill.
	tw, _ := w.Variable("temp")
	if err := tw.WriteSlice([]int32{99, 98}, []int64{2, 0}, []int64{1, 2}); err != nil {
		t.Fatalf("WriteSlice: %v", err)
	}
	if got := recs(t, w, "temp"); got != 3 {
		t.Fatalf("record count = %d, want 3", got)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	f, err := netcdf.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = f.Close() }()
	v, _ := f.Variable("temp")
	temp := make([]int32, v.Len())
	if err := v.Read(&temp); err != nil {
		t.Fatal(err)
	}
	wantTemp := []int32{-2147483647, -2147483647, -2147483647, -2147483647, 99, 98}
	for i := range wantTemp {
		if temp[i] != wantTemp[i] {
			t.Errorf("temp[%d] = %d, want %d", i, temp[i], wantTemp[i])
		}
	}
	fv, _ := f.Variable("flag")
	flag := make([]int16, fv.Len())
	if err := fv.Read(&flag); err != nil {
		t.Fatal(err)
	}
	for i := range flag {
		if flag[i] != -32767 {
			t.Errorf("flag[%d] = %d, want the fill value", i, flag[i])
		}
	}
}

// TestWriterFillValue proves _FillValue overrides the type's default for the
// initial fill and for appended records.
func TestWriterFillValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.nc")
	s := netcdf.NewSchema(netcdf.FormatClassic)
	time := s.AddDimension("time", 0)
	x := s.AddDimension("x", 2)
	fixed := s.AddVariable("fixed", netcdf.TypeInt, x)
	mustAttr(t, fixed.SetAttr("_FillValue", []int32{42}))
	rec := s.AddVariable("rec", netcdf.TypeInt, time, x)
	mustAttr(t, rec.SetAttr("_FillValue", []int32{-7}))
	w, err := netcdf.Create(path, s)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	recw, ok := w.Variable("rec")
	if !ok {
		t.Fatal("rec not found")
	}
	if err := recw.WriteSlice([]int32{1, 2}, []int64{1, 0}, []int64{1, 2}); err != nil {
		t.Fatalf("WriteSlice: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	f, err := netcdf.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = f.Close() }()
	fv, _ := f.Variable("fixed")
	fixedVals := make([]int32, fv.Len())
	if err := fv.Read(&fixedVals); err != nil {
		t.Fatal(err)
	}
	if fixedVals[0] != 42 || fixedVals[1] != 42 {
		t.Errorf("fixed = %v, want the _FillValue 42 everywhere", fixedVals)
	}
	rv, _ := f.Variable("rec")
	recVals := make([]int32, rv.Len())
	if err := rv.Read(&recVals); err != nil {
		t.Fatal(err)
	}
	want := []int32{-7, -7, 1, 2}
	for i := range want {
		if recVals[i] != want[i] {
			t.Errorf("rec[%d] = %d, want %d", i, recVals[i], want[i])
		}
	}
}

// TestWriterValidationCoversSchema walks the schema rules Create enforces.
func TestWriterValidationCoversSchema(t *testing.T) {
	cases := []struct {
		name  string
		build func(t *testing.T) *netcdf.Schema
		want  error
	}{
		{"two unlimited dimensions", func(_ *testing.T) *netcdf.Schema {
			s := netcdf.NewSchema(netcdf.FormatClassic)
			s.AddDimension("a", 0)
			s.AddDimension("b", 0)
			return s
		}, netcdf.ErrInvalidSchema},
		{"unknown dimension", func(_ *testing.T) *netcdf.Schema {
			s := netcdf.NewSchema(netcdf.FormatClassic)
			foreign := netcdf.NewSchema(netcdf.FormatClassic).AddDimension("x", 2)
			s.AddVariable("v", netcdf.TypeInt, foreign)
			return s
		}, netcdf.ErrInvalidSchema},
		{"cdf5 type in cdf1", func(_ *testing.T) *netcdf.Schema {
			s := netcdf.NewSchema(netcdf.FormatClassic)
			x := s.AddDimension("x", 2)
			s.AddVariable("v", netcdf.TypeUByte, x)
			return s
		}, netcdf.ErrInvalidSchema},
		{"duplicate variable", func(_ *testing.T) *netcdf.Schema {
			s := netcdf.NewSchema(netcdf.FormatClassic)
			x := s.AddDimension("x", 2)
			s.AddVariable("v", netcdf.TypeInt, x)
			s.AddVariable("v", netcdf.TypeInt, x)
			return s
		}, netcdf.ErrInvalidSchema},
		{"bad name", func(_ *testing.T) *netcdf.Schema {
			s := netcdf.NewSchema(netcdf.FormatClassic)
			s.AddDimension("a/b", 2)
			return s
		}, netcdf.ErrInvalidSchema},
		{"fill value of another type", func(t *testing.T) *netcdf.Schema {
			s := netcdf.NewSchema(netcdf.FormatClassic)
			x := s.AddDimension("x", 2)
			v := s.AddVariable("v", netcdf.TypeInt, x)
			mustAttr(t, v.SetAttr("_FillValue", []float64{1}))
			return s
		}, netcdf.ErrInvalidSchema},
		{"hdf5 format", func(_ *testing.T) *netcdf.Schema {
			return netcdf.NewSchema(netcdf.FormatHDF5)
		}, netcdf.ErrUnsupportedFormat},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "out.nc")
			_, err := netcdf.Create(path, c.build(t))
			if !errors.Is(err, c.want) {
				t.Fatalf("Create = %v, want %v", err, c.want)
			}
			if _, statErr := os.Stat(path); statErr == nil {
				t.Errorf("Create left a file behind after a schema error")
			}
		})
	}
}

// TestWriterUsageErrors covers the writer's runtime contract: type and length
// checks on Write/WriteSlice, writes after Close.
func TestWriterUsageErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.nc")
	s := netcdf.NewSchema(netcdf.FormatClassic)
	x := s.AddDimension("x", 3)
	s.AddVariable("v", netcdf.TypeInt, x)
	w, err := netcdf.Create(path, s)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	v, _ := w.Variable("v")
	if err := v.Write([]int16{1, 2, 3}); !errors.Is(err, netcdf.ErrTypeMismatch) {
		t.Errorf("Write with a wrong element type = %v, want ErrTypeMismatch", err)
	}
	if err := v.Write([]int32{1, 2}); !errors.Is(err, netcdf.ErrOutOfRange) {
		t.Errorf("Write with a short slice = %v, want ErrOutOfRange", err)
	}
	if err := v.WriteSlice([]int32{1}, []int64{2}, []int64{2}); !errors.Is(err, netcdf.ErrOutOfRange) {
		t.Errorf("WriteSlice past the end = %v, want ErrOutOfRange", err)
	}
	if err := v.Write([]int32{1, 2, 3}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if err := v.Write([]int32{1, 2, 3}); !errors.Is(err, netcdf.ErrClosed) {
		t.Errorf("Write after Close = %v, want ErrClosed", err)
	}
}
