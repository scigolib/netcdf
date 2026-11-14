package netcdf_test

import (
	"path/filepath"
	"testing"

	"github.com/scigolib/netcdf"
)

// TestChunkedReadWrite crosses the internal 16384-element chunk boundary in
// both directions: one whole-variable write and read of 20000 values, and a
// slab that starts before the boundary and ends after it.
func TestChunkedReadWrite(t *testing.T) {
	const n = 20000 // > readChunkElems and writeChunkElems (16384)
	path := filepath.Join(t.TempDir(), "chunk.nc")

	schema := netcdf.NewSchema(netcdf.FormatClassic)
	x := schema.AddDimension("x", n)
	schema.AddVariable("v", netcdf.TypeInt, x)
	w, err := netcdf.Create(path, schema)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	values := make([]int32, n)
	for i := range values {
		values[i] = int32(i*3 - 7)
	}
	vw, ok := w.Variable("v")
	if !ok {
		t.Fatal(`Variable("v") not found`)
	}
	if err := vw.Write(values); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	f, err := netcdf.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	v, ok := f.Variable("v")
	if !ok {
		t.Fatal(`Variable("v") not found`)
	}
	got := make([]int32, n)
	if err := v.Read(&got); err != nil {
		t.Fatalf("Read: %v", err)
	}
	for i := range got {
		if got[i] != values[i] {
			t.Fatalf("whole read: value %d = %d, want %d", i, got[i], values[i])
		}
	}

	const start, count = 16000, 1000
	slab := make([]int32, count)
	if err := v.ReadSlice(&slab, []int64{start}, []int64{count}); err != nil {
		t.Fatalf("ReadSlice: %v", err)
	}
	for i := range slab {
		if want := values[start+i]; slab[i] != want {
			t.Fatalf("slab value %d = %d, want %d", start+i, slab[i], want)
		}
	}
}
