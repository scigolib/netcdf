package netcdf_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/scigolib/netcdf"
)

// eofAtEndReaderAt returns io.EOF whenever a read ends exactly at the end of
// the input, which io.ReaderAt permits. *os.File never does, so nothing else
// in the suite exercises that case.
type eofAtEndReaderAt struct {
	f    *os.File
	size int64
}

func (r *eofAtEndReaderAt) ReadAt(p []byte, off int64) (int, error) {
	n, err := r.f.ReadAt(p, off)
	if err == nil && off+int64(n) == r.size {
		return n, io.EOF
	}
	return n, err
}

// errFail is the sentinel an I/O failure returns; the package must let it
// through rather than report it as corruption.
var errFail = errors.New("reader failed")

// failReaderAt fails every read that starts at or after failAt.
type failReaderAt struct {
	f      *os.File
	failAt int64
}

func (r *failReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off >= r.failAt {
		return 0, errFail
	}
	return r.f.ReadAt(p, off)
}

// shortReaderAt truncates the last byte of a read that would end at the end of
// the input and reports io.EOF with the short count.
type shortReaderAt struct {
	f    *os.File
	size int64
}

func (r *shortReaderAt) ReadAt(p []byte, off int64) (int, error) {
	n, err := r.f.ReadAt(p, off)
	if err == nil && off+int64(n) == r.size {
		return n - 1, io.EOF
	}
	return n, err
}

func openNamed(t *testing.T, path string) (*os.File, int64) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	st, err := f.Stat()
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	return f, st.Size()
}

func wantIOError(t *testing.T, err error, what string) {
	t.Helper()
	if !errors.Is(err, errFail) {
		t.Fatalf("%s: %v, want the I/O error", what, err)
	}
	if errors.Is(err, netcdf.ErrCorrupt) {
		t.Fatalf("%s: %v, want it not to be ErrCorrupt", what, err)
	}
}

// TestFullReadAtEOF opens and reads a fixture through a reader that reports
// io.EOF together with every complete read ending at the end of the input.
// Both the header read (the whole 356-byte file fits one window) and the read
// of the last fixed variable end there.
func TestFullReadAtEOF(t *testing.T) {
	f, size := openNamed(t, "testdata/simple-cdf1.nc")
	nf, err := netcdf.NewReader(&eofAtEndReaderAt{f: f, size: size}, size)
	if err != nil {
		t.Fatalf("NewReader over a reader that returns (n, io.EOF) at the end: %v", err)
	}
	t.Cleanup(func() {
		if err := nf.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	b, ok := nf.Variable("b")
	if !ok {
		t.Fatal(`Variable("b") not found`)
	}
	dst := make([]int32, b.Len())
	if err := b.Read(&dst); err != nil {
		t.Fatalf("Read(b), whose data ends at EOF: %v", err)
	}
	if len(dst) != 3 || dst[0] != 10 || dst[1] != 20 || dst[2] != 30 {
		t.Fatalf("b = %v, want [10 20 30]", dst)
	}
}

// TestReadErrorDetect fails the format-detection read: the error must come
// through as itself.
func TestReadErrorDetect(t *testing.T) {
	f, size := openNamed(t, "testdata/simple-cdf1.nc")
	_, err := netcdf.NewReader(&failReaderAt{f: f, failAt: 0}, size)
	wantIOError(t, err, "NewReader")
}

// TestReadErrorHeaderWindow fails the header cursor's second window read (the
// header is larger than the 32 KiB window): the error must come through as
// itself, not as corruption.
func TestReadErrorHeaderWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.nc")
	schema := netcdf.NewSchema(netcdf.FormatClassic)
	schema.SetAttr("blob", make([]byte, 40<<10))
	w, err := netcdf.Create(path, schema)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	f, size := openNamed(t, path)
	_, err = netcdf.NewReader(&failReaderAt{f: f, failAt: 32 << 10}, size)
	wantIOError(t, err, "NewReader")
}

// TestReadErrorData fails the read of the last fixed variable, whose data ends
// at the end of the file.
func TestReadErrorData(t *testing.T) {
	f, size := openNamed(t, "testdata/simple-cdf1.nc")
	// b holds three int32 values at the end of the file; fail the read that
	// starts there.
	nf, err := netcdf.NewReader(&failReaderAt{f: f, failAt: size - 12}, size)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	t.Cleanup(func() {
		if err := nf.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	b, ok := nf.Variable("b")
	if !ok {
		t.Fatal(`Variable("b") not found`)
	}
	dst := make([]int32, b.Len())
	wantIOError(t, b.Read(&dst), "Read")
}

// TestShortReadAtEnd reads through a reader that returns one byte less than
// asked at the end of the input: a truncated file is corruption.
func TestShortReadAtEnd(t *testing.T) {
	f, size := openNamed(t, "testdata/simple-cdf1.nc")
	_, err := netcdf.NewReader(&shortReaderAt{f: f, size: size}, size)
	if !errors.Is(err, netcdf.ErrCorrupt) {
		t.Fatalf("NewReader: %v, want ErrCorrupt", err)
	}
}
