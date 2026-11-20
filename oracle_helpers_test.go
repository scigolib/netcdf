//go:build oracle

package netcdf_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/scigolib/netcdf"
)

// oracleBinEnv names the directory that holds ncgen, ncdump and nccopy. When it
// is unset the tools are looked up on PATH. A run with the oracle tag never
// skips: a missing tool is a failure, because a skipped oracle is a hole in the
// evidence, not a pass.
const oracleBinEnv = "NETCDF_ORACLE_BIN"

// oracleTool returns the path of one reference tool or fails the test.
func oracleTool(t *testing.T, name string) string {
	t.Helper()
	if dir := os.Getenv(oracleBinEnv); dir != "" {
		p := filepath.Join(dir, name)
		info, err := os.Stat(p)
		if err != nil || info.IsDir() {
			t.Fatalf("oracle: %s=%q does not hold %s", oracleBinEnv, dir, name)
		}
		return p
	}
	p, err := exec.LookPath(name)
	if err != nil {
		t.Fatalf("oracle: %s not found on PATH: install netcdf-bin or set %s to the directory that holds ncgen, ncdump and nccopy", name, oracleBinEnv)
	}
	return p
}

// runOracle runs a reference tool and returns its standard output. Any failure
// is fatal for the subtest: without the oracle's answer there is no verdict.
func runOracle(t *testing.T, bin string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("oracle: %s %v: %v\n%s", filepath.Base(bin), args, err, stderr.String())
	}
	return string(out)
}

// writeThroughOurAPI reads src with the public reader and writes dst with the
// public writer, so the schema and every value cross the public API unchanged.
// The oracle then compares the two files through ncdump and nccopy.
func writeThroughOurAPI(t *testing.T, src, dst string) {
	t.Helper()
	f, err := netcdf.Open(src)
	if err != nil {
		t.Fatalf("reading the reference file %s: %v", filepath.Base(src), err)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Errorf("closing %s: %v", src, err)
		}
	})

	w, err := netcdf.Create(dst, schemaFromFile(t, f))
	if err != nil {
		t.Fatalf("creating %s: %v", filepath.Base(dst), err)
	}
	for _, v := range f.Variables() {
		values := readWholeVariable(t, v)
		if values == nil {
			continue // a variable with no values has nothing to write
		}
		vw, ok := w.Variable(v.Name())
		if !ok {
			t.Fatalf("the writer lost variable %q", v.Name())
		}
		if err := writeTyped(t, vw, values); err != nil {
			t.Fatalf("writing %q: %v", v.Name(), err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("closing %s: %v", filepath.Base(dst), err)
	}
}

// schemaFromFile rebuilds the file's schema through the public API. The
// definition order of dimensions, variables and attributes is preserved:
// ncdump prints in file order, so the comparison depends on it.
func schemaFromFile(t *testing.T, f *netcdf.File) *netcdf.Schema {
	t.Helper()
	s := netcdf.NewSchema(f.Format())
	dims := make(map[string]*netcdf.SchemaDim, len(f.Dimensions()))
	for _, d := range f.Dimensions() {
		length := d.Len()
		if d.Unlimited() {
			length = 0 // a length of zero marks the unlimited dimension
		}
		dims[d.Name()] = s.AddDimension(d.Name(), length)
	}
	for _, v := range f.Variables() {
		vdims := make([]*netcdf.SchemaDim, len(v.Dims()))
		for i, d := range v.Dims() {
			dim, ok := dims[d.Name()]
			if !ok {
				t.Fatalf("variable %q uses dimension %q that the file does not define", v.Name(), d.Name())
			}
			vdims[i] = dim
		}
		sv := s.AddVariable(v.Name(), v.Type(), vdims...)
		for _, a := range v.Attributes() {
			if err := sv.SetAttrTyped(a.Name(), a.Type(), a.Value()); err != nil {
				t.Fatalf("variable %q attribute %q: %v", v.Name(), a.Name(), err)
			}
		}
	}
	for _, a := range f.Attributes() {
		if err := s.SetAttrTyped(a.Name(), a.Type(), a.Value()); err != nil {
			t.Fatalf("global attribute %q: %v", a.Name(), err)
		}
	}
	return s
}

// readWholeVariable reads every value of v in the exact Go element type its
// netCDF type maps to. A nil result means the variable holds no values.
func readWholeVariable(t *testing.T, v *netcdf.Variable) any {
	t.Helper()
	n := v.Len()
	if n == 0 {
		return nil
	}
	switch v.Type() {
	case netcdf.TypeByte:
		p := make([]int8, n)
		mustRead(t, v, &p)
		return p
	case netcdf.TypeChar, netcdf.TypeUByte:
		p := make([]byte, n)
		mustRead(t, v, &p)
		return p
	case netcdf.TypeShort:
		p := make([]int16, n)
		mustRead(t, v, &p)
		return p
	case netcdf.TypeUShort:
		p := make([]uint16, n)
		mustRead(t, v, &p)
		return p
	case netcdf.TypeInt:
		p := make([]int32, n)
		mustRead(t, v, &p)
		return p
	case netcdf.TypeUInt:
		p := make([]uint32, n)
		mustRead(t, v, &p)
		return p
	case netcdf.TypeFloat:
		p := make([]float32, n)
		mustRead(t, v, &p)
		return p
	case netcdf.TypeDouble:
		p := make([]float64, n)
		mustRead(t, v, &p)
		return p
	case netcdf.TypeInt64:
		p := make([]int64, n)
		mustRead(t, v, &p)
		return p
	case netcdf.TypeUInt64:
		p := make([]uint64, n)
		mustRead(t, v, &p)
		return p
	default:
		t.Fatalf("variable %q has type %s with no Go mapping", v.Name(), v.Type())
	}
	return nil
}

func mustRead[T any](t *testing.T, v *netcdf.Variable, dst *[]T) {
	t.Helper()
	if err := v.Read(dst); err != nil {
		t.Fatalf("reading %q: %v", v.Name(), err)
	}
}
