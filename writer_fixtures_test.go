package netcdf_test

// Byte-for-byte parity between our writer and netCDF-C's ncgen: every model
// of testdata/cdl is rebuilt through the public API and compared with the
// committed fixture for every format the oracle could produce. A difference is
// our defect or a deliberate, recorded deviation.

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/scigolib/netcdf"
)

func mustAttr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("SetAttr: %v", err)
	}
}

func mustWVar(t *testing.T, w *netcdf.Writer, name string) *netcdf.VarWriter {
	t.Helper()
	v, ok := w.Variable(name)
	if !ok {
		t.Fatalf("variable %q not found", name)
	}
	return v
}

func mustWrite(t *testing.T, v *netcdf.VarWriter, values any) {
	t.Helper()
	if err := v.Write(values); err != nil {
		t.Fatalf("write %q: %v", v.Name(), err)
	}
}

func buildSimple(t *testing.T, format netcdf.Format) *netcdf.Schema {
	s := netcdf.NewSchema(format)
	x := s.AddDimension("x", 4)
	y := s.AddDimension("y", 3)
	a := s.AddVariable("a", netcdf.TypeFloat, x, y)
	mustAttr(t, a.SetAttr("units", "m/s"))
	mustAttr(t, a.SetAttr("valid_max", []float64{100}))
	b := s.AddVariable("b", netcdf.TypeInt, y)
	mustAttr(t, b.SetAttr("scale", []int32{2}))
	mustAttr(t, s.SetAttr("title", "simple fixture"))
	mustAttr(t, s.SetAttr("version", []int32{7}))
	mustAttr(t, s.SetAttr("pi", []float64{3.14159}))
	return s
}

func writeSimple(t *testing.T, w *netcdf.Writer) {
	mustWrite(t, mustWVar(t, w, "a"), []float32{
		0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11,
	})
	mustWrite(t, mustWVar(t, w, "b"), []int32{10, 20, 30})
}

func buildTypes(_ *testing.T, format netcdf.Format) *netcdf.Schema {
	s := netcdf.NewSchema(format)
	n := s.AddDimension("n", 5)
	s.AddVariable("vb", netcdf.TypeByte, n)
	s.AddVariable("vc", netcdf.TypeChar, n)
	s.AddVariable("vs", netcdf.TypeShort, n)
	s.AddVariable("vi", netcdf.TypeInt, n)
	s.AddVariable("vf", netcdf.TypeFloat, n)
	s.AddVariable("vd", netcdf.TypeDouble, n)
	return s
}

func writeTypes(t *testing.T, w *netcdf.Writer) {
	mustWrite(t, mustWVar(t, w, "vb"), []int8{-128, -1, 0, 1, 127})
	mustWrite(t, mustWVar(t, w, "vc"), []byte("hello"))
	mustWrite(t, mustWVar(t, w, "vs"), []int16{-32768, -1, 0, 1, 32767})
	mustWrite(t, mustWVar(t, w, "vi"), []int32{-2147483648, -1, 0, 1, 2147483647})
	mustWrite(t, mustWVar(t, w, "vf"), []float32{-1.5, 0, 0.5, 1, 2.25})
	mustWrite(t, mustWVar(t, w, "vd"), []float64{-1.5, 0, 0.5, 1, 2.25})
}

func buildRecord(_ *testing.T, format netcdf.Format) *netcdf.Schema {
	s := netcdf.NewSchema(format)
	time := s.AddDimension("time", 0)
	x := s.AddDimension("x", 2)
	s.AddVariable("fixed", netcdf.TypeDouble, x)
	s.AddVariable("temp", netcdf.TypeInt, time, x)
	s.AddVariable("flag", netcdf.TypeShort, time)
	return s
}

func writeRecord(t *testing.T, w *netcdf.Writer) {
	mustWrite(t, mustWVar(t, w, "fixed"), []float64{1.5, 2.5})
	mustWrite(t, mustWVar(t, w, "temp"), []int32{10, 11, 20, 21, 30, 31})
	mustWrite(t, mustWVar(t, w, "flag"), []int16{1, 0, -1})
}

func buildLayout(_ *testing.T, format netcdf.Format) *netcdf.Schema {
	s := netcdf.NewSchema(format)
	time := s.AddDimension("time", 0)
	x := s.AddDimension("x", 3)
	y := s.AddDimension("y", 2)
	z := s.AddDimension("z", 4)
	s.AddVariable("cube", netcdf.TypeFloat, z, y, x)
	s.AddVariable("b", netcdf.TypeByte, time)
	s.AddVariable("temp", netcdf.TypeInt, time, x)
	return s
}

func writeLayout(t *testing.T, w *netcdf.Writer) {
	cube := make([]float32, 24)
	for i := range cube {
		cube[i] = float32(i)
	}
	mustWrite(t, mustWVar(t, w, "cube"), cube)
	mustWrite(t, mustWVar(t, w, "b"), []int8{1, -1, 2, -2, 5})
	mustWrite(t, mustWVar(t, w, "temp"), []int32{
		10, 11, 12, 20, 21, 22, 30, 31, 32, 40, 41, 42, 50, 51, 52,
	})
}

func buildAttrs(t *testing.T, format netcdf.Format) *netcdf.Schema {
	s := netcdf.NewSchema(format)
	d := s.AddDimension("d", 2)
	v := s.AddVariable("v", netcdf.TypeInt, d)
	mustAttr(t, v.SetAttr("units", "count"))
	mustAttr(t, v.SetAttr("valid_range", []int32{0, 100}))
	mustAttr(t, s.SetAttr("title", "attribute types"))
	mustAttr(t, s.SetAttr("reals", []float64{1.5, -2.25}))
	return s
}

func writeAttrs(t *testing.T, w *netcdf.Writer) {
	mustWrite(t, mustWVar(t, w, "v"), []int32{3, 4})
}

// buildFill declares variables and writes nothing: the file holds only the
// initial fill, which is the one path a data-carrying model overwrites.
func buildFill(t *testing.T, format netcdf.Format) *netcdf.Schema {
	s := netcdf.NewSchema(format)
	n := s.AddDimension("n", 3)
	s.AddVariable("b", netcdf.TypeByte, n)
	s.AddVariable("c", netcdf.TypeChar, n)
	s.AddVariable("s", netcdf.TypeShort, n)
	s.AddVariable("i", netcdf.TypeInt, n)
	s.AddVariable("f", netcdf.TypeFloat, n)
	s.AddVariable("d", netcdf.TypeDouble, n)
	mustAttr(t, s.SetAttr("note", "unwritten variables keep their fill values"))
	return s
}

func writeNothing(*testing.T, *netcdf.Writer) {}

// buildCDF5Types covers the five types only CDF-5 has; ncgen cannot produce
// them in the classic-only oracle build, so the fixture is the writer's own
// output that ncdump read (testdata/README.md).
func buildCDF5Types(_ *testing.T, format netcdf.Format) *netcdf.Schema {
	s := netcdf.NewSchema(format)
	n := s.AddDimension("n", 3)
	s.AddVariable("ub", netcdf.TypeUByte, n)
	s.AddVariable("us", netcdf.TypeUShort, n)
	s.AddVariable("ui", netcdf.TypeUInt, n)
	s.AddVariable("i64", netcdf.TypeInt64, n)
	s.AddVariable("u64", netcdf.TypeUInt64, n)
	return s
}

func writeCDF5Types(t *testing.T, w *netcdf.Writer) {
	mustWrite(t, mustWVar(t, w, "ub"), []byte{0, 128, 255})
	mustWrite(t, mustWVar(t, w, "us"), []uint16{0, 32768, 65534})
	mustWrite(t, mustWVar(t, w, "ui"), []uint32{0, 2147483648, 4294967294})
	mustWrite(t, mustWVar(t, w, "i64"), []int64{-9223372036854775807, 0, 9223372036854775807})
	mustWrite(t, mustWVar(t, w, "u64"), []uint64{0, 9223372036854775808, 18446744073709551615})
}

func buildSingleRec(_ *testing.T, format netcdf.Format) *netcdf.Schema {
	s := netcdf.NewSchema(format)
	time := s.AddDimension("time", 0)
	s.AddVariable("b", netcdf.TypeByte, time)
	return s
}

func writeSingleRec(t *testing.T, w *netcdf.Writer) {
	mustWrite(t, mustWVar(t, w, "b"), []int8{7, -7, 0, 42, -42})
}

// TestCreateMatchesFixtures is the writer's acceptance bar: the file we write
// is byte-identical to the one ncgen wrote for the same model and format.
func TestCreateMatchesFixtures(t *testing.T) {
	cases := []struct {
		model   string
		format  netcdf.Format
		build   func(t *testing.T, format netcdf.Format) *netcdf.Schema
		write   func(t *testing.T, w *netcdf.Writer)
		fixture string
	}{
		{"simple", netcdf.FormatClassic, buildSimple, writeSimple, "simple-cdf1.nc"},
		{"simple", netcdf.FormatClassic64, buildSimple, writeSimple, "simple-cdf2.nc"},
		{"simple", netcdf.FormatClassic64Data, buildSimple, writeSimple, "simple-cdf5.nc"},
		{"types", netcdf.FormatClassic, buildTypes, writeTypes, "types-cdf1.nc"},
		{"types", netcdf.FormatClassic64, buildTypes, writeTypes, "types-cdf2.nc"},
		{"record", netcdf.FormatClassic, buildRecord, writeRecord, "record-cdf1.nc"},
		{"record", netcdf.FormatClassic64, buildRecord, writeRecord, "record-cdf2.nc"},
		{"record", netcdf.FormatClassic64Data, buildRecord, writeRecord, "record-cdf5.nc"},
		{"layout", netcdf.FormatClassic, buildLayout, writeLayout, "layout-cdf1.nc"},
		{"layout", netcdf.FormatClassic64, buildLayout, writeLayout, "layout-cdf2.nc"},
		{"layout", netcdf.FormatClassic64Data, buildLayout, writeLayout, "layout-cdf5.nc"},
		{"attrs", netcdf.FormatClassic, buildAttrs, writeAttrs, "attrs-cdf1.nc"},
		{"fill", netcdf.FormatClassic, buildFill, writeNothing, "fill-cdf1.nc"},
		{"fill", netcdf.FormatClassic64, buildFill, writeNothing, "fill-cdf2.nc"},
		{"fill", netcdf.FormatClassic64Data, buildFill, writeNothing, "fill-cdf5.nc"},
		{"cdf5types", netcdf.FormatClassic64Data, buildCDF5Types, writeCDF5Types, "cdf5types-cdf5.nc"},
		{"single_rec", netcdf.FormatClassic, buildSingleRec, writeSingleRec, "single_rec-cdf1.nc"},
	}
	for _, c := range cases {
		t.Run(c.model+"-"+c.format.String(), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "out.nc")
			w, err := netcdf.Create(path, c.build(t, c.format))
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			c.write(t, w)
			if err := w.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join("testdata", c.fixture))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("output differs from %s: got %d bytes, want %d", c.fixture, len(got), len(want))
			}
		})
	}
}

// TestCreateEmpty covers the model ncgen pads to 4096 bytes. Our file ends at
// the header instead — a deliberate deviation — and the header bytes must be
// identical.
func TestCreateEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.nc")
	w, err := netcdf.Create(path, netcdf.NewSchema(netcdf.FormatClassic))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "empty-cdf1.nc"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 32 {
		t.Fatalf("empty file is %d bytes, the header is 32", len(got))
	}
	if !bytes.Equal(got, want[:32]) {
		t.Fatalf("empty header differs:\n got % x\nwant % x", got, want[:32])
	}
}
