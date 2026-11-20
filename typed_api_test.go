package netcdf_test

import (
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/scigolib/netcdf"
)

// ExampleVariable_Read_dynamic shows the pattern for data whose element type is
// known only at run time: switch on Variable.Type and call the typed method per
// arm. The same switch is how a dynamic consumer builds its typed destination.
func ExampleVariable_Read_dynamic() {
	f, err := netcdf.Open("data.nc")
	if err != nil {
		log.Fatal(err)
	}

	for _, v := range f.Variables() {
		switch v.Type() {
		case netcdf.TypeFloat:
			values := make([]float32, v.Len())
			if err := v.Read(&values); err != nil {
				log.Fatal(err)
			}
			fmt.Println(v.Name(), values)
		case netcdf.TypeDouble:
			values := make([]float64, v.Len())
			if err := v.Read(&values); err != nil {
				log.Fatal(err)
			}
			fmt.Println(v.Name(), values)
		case netcdf.TypeInt:
			values := make([]int32, v.Len())
			if err := v.Read(&values); err != nil {
				log.Fatal(err)
			}
			fmt.Println(v.Name(), values)
		default:
			log.Fatalf("%s: no example for type %s", v.Name(), v.Type())
		}
	}

	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
}

// TestReadDynamicTypeSwitch exercises the documented pattern end to end: a file
// with variables of several types is read through one type switch over
// Variable.Type, each arm calling the typed method.
func TestReadDynamicTypeSwitch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dynamic.nc")
	s := netcdf.NewSchema(netcdf.FormatClassic64Data)
	n := s.AddDimension("n", 3)
	s.AddVariable("f", netcdf.TypeFloat, n)
	s.AddVariable("d", netcdf.TypeDouble, n)
	s.AddVariable("i", netcdf.TypeInt, n)
	s.AddVariable("b", netcdf.TypeByte, n)
	w, err := netcdf.Create(path, s)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	writeTyped(t, mustWVar(t, w, "f"), []float32{1.5, -2.5, 3.25})
	writeTyped(t, mustWVar(t, w, "d"), []float64{1.5, -2.5, 3.25})
	writeTyped(t, mustWVar(t, w, "i"), []int32{1, -2, 3})
	writeTyped(t, mustWVar(t, w, "b"), []int8{1, -2, 3})
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	f, err := netcdf.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = f.Close() }()

	got := make(map[string]any, len(f.Variables()))
	for _, v := range f.Variables() {
		switch v.Type() {
		case netcdf.TypeFloat:
			values := make([]float32, v.Len())
			if err := v.Read(&values); err != nil {
				t.Fatalf("reading %q: %v", v.Name(), err)
			}
			got[v.Name()] = values
		case netcdf.TypeDouble:
			values := make([]float64, v.Len())
			if err := v.Read(&values); err != nil {
				t.Fatalf("reading %q: %v", v.Name(), err)
			}
			got[v.Name()] = values
		case netcdf.TypeInt:
			values := make([]int32, v.Len())
			if err := v.Read(&values); err != nil {
				t.Fatalf("reading %q: %v", v.Name(), err)
			}
			got[v.Name()] = values
		case netcdf.TypeByte:
			values := make([]int8, v.Len())
			if err := v.Read(&values); err != nil {
				t.Fatalf("reading %q: %v", v.Name(), err)
			}
			got[v.Name()] = values
		default:
			t.Fatalf("unhandled type %s", v.Type())
		}
	}
	want := map[string]any{
		"f": []float32{1.5, -2.5, 3.25},
		"d": []float64{1.5, -2.5, 3.25},
		"i": []int32{1, -2, 3},
		"b": []int8{1, -2, 3},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dynamic read = %v, want %v", got, want)
	}
}

// TestTypedDataPathMismatch proves the runtime check behind the typed methods:
// the element type compiles, but it does not match the variable's type.
func TestTypedDataPathMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mismatch.nc")
	s := netcdf.NewSchema(netcdf.FormatClassic)
	n := s.AddDimension("n", 2)
	s.AddVariable("v", netcdf.TypeFloat, n)
	w, err := netcdf.Create(path, s)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	vw := mustWVar(t, w, "v")
	if err := vw.Write([]float64{1, 2}); !errors.Is(err, netcdf.ErrTypeMismatch) {
		t.Errorf("Write []float64 into a float variable = %v, want ErrTypeMismatch", err)
	}
	if err := vw.Write([]float32{1, 2}); err != nil {
		t.Fatalf("Write []float32: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	f, err := netcdf.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = f.Close() }()
	v := mustVar(t, f, "v")

	var wrong []float64
	if err := v.Read(&wrong); !errors.Is(err, netcdf.ErrTypeMismatch) {
		t.Errorf("Read into []float64 = %v, want ErrTypeMismatch", err)
	}
	var wrongSlice []float64
	if err := v.ReadSlice(&wrongSlice, []int64{0}, []int64{2}); !errors.Is(err, netcdf.ErrTypeMismatch) {
		t.Errorf("ReadSlice into []float64 = %v, want ErrTypeMismatch", err)
	}
	right := make([]float32, v.Len())
	if err := v.Read(&right); err != nil {
		t.Fatalf("Read into []float32: %v", err)
	}
	wantSlice(t, "v", right, []float32{1, 2})
}

// TestTypeStringReserved proves TypeString is a reserved constant of the
// nc_type value space: it exists, it prints, and the classic paths reject it.
func TestTypeStringReserved(t *testing.T) {
	if netcdf.TypeString != 12 {
		t.Errorf("TypeString = %d, want 12", netcdf.TypeString)
	}
	if got := netcdf.TypeString.String(); got != "string" {
		t.Errorf("TypeString.String() = %q, want string", got)
	}
	if got := netcdf.TypeString.Size(); got != 0 {
		t.Errorf("TypeString.Size() = %d, want 0", got)
	}
	if got := netcdf.Type(1000).String(); got != "Type(1000)" {
		t.Errorf("Type(1000).String() = %q, want Type(1000)", got)
	}

	path := filepath.Join(t.TempDir(), "bad.nc")
	s := netcdf.NewSchema(netcdf.FormatClassic)
	n := s.AddDimension("n", 1)
	s.AddVariable("s", netcdf.TypeString, n)
	if _, err := netcdf.Create(path, s); !errors.Is(err, netcdf.ErrInvalidSchema) {
		t.Fatalf("Create with a TypeString variable = %v, want ErrInvalidSchema", err)
	}

	s2 := netcdf.NewSchema(netcdf.FormatClassic)
	if err := s2.SetAttrTyped("a", netcdf.TypeString, []string{"x"}); !errors.Is(err, netcdf.ErrInvalidSchema) {
		t.Fatalf("SetAttrTyped with TypeString = %v, want ErrInvalidSchema", err)
	}
}
