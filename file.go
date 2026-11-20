package netcdf

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync/atomic"

	"github.com/scigolib/netcdf/internal/cdf"
)

// File is an open NetCDF file. Its metadata (dimensions, attributes,
// variables) is parsed once when the file is opened; data is read on demand.
//
// A File may be read from several goroutines at once. Close must not run
// concurrently with reads.
type File struct {
	r      io.ReaderAt
	size   int64
	closer io.Closer // nil when the file was opened with NewReader
	h      *cdf.Header
	format Format

	dims  []*Dimension
	attrs []*Attribute
	vars  []*Variable

	dimByName  map[string]*Dimension
	attrByName map[string]*Attribute
	varByName  map[string]*Variable

	closed atomic.Bool
}

// Open opens the NetCDF file at path for reading. It recognizes the classic
// formats (CDF-1, CDF-2, CDF-5) and reports ErrUnsupportedFormat for a
// NetCDF-4 file, whose HDF5 layer is not wired up yet.
//
// The returned File owns the underlying file; Close releases it.
func Open(path string) (*File, error) {
	// Opening the caller's path is the function's purpose; the path is an
	// argument, not an untrusted include.
	osf, err := os.Open(path) //nolint:gosec // G304: the path is this function's argument by design
	if err != nil {
		return nil, err
	}
	st, err := osf.Stat()
	if err != nil {
		// Closing here only releases a descriptor; the stat error is the one
		// that explains the failure.
		_ = osf.Close()
		return nil, err
	}
	if st.IsDir() {
		_ = osf.Close()
		return nil, fmt.Errorf("netcdf: %s is a directory", path)
	}
	f, err := newFile(osf, st.Size(), osf)
	if err != nil {
		_ = osf.Close()
		return nil, err
	}
	return f, nil
}

// NewReader reads a NetCDF file from r, whose content is size bytes long. The
// caller keeps ownership of r: Close does not close it.
//
// NewReader is what makes reading from memory, a section of a larger file, or
// a test fixture possible without a temporary file.
func NewReader(r io.ReaderAt, size int64) (*File, error) {
	return newFile(r, size, nil)
}

func newFile(r io.ReaderAt, size int64, closer io.Closer) (*File, error) {
	if r == nil {
		return nil, errors.New("netcdf: nil reader")
	}
	if size < 0 {
		return nil, fmt.Errorf("netcdf: negative size %d", size)
	}
	n := int64(len("\x89HDF\r\n\x1a\n"))
	if size < n {
		n = size
	}
	head := make([]byte, n)
	if n > 0 {
		if _, err := r.ReadAt(head, 0); err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
	}
	switch format := DetectFormat(head); format {
	case FormatClassic, FormatClassic64, FormatClassic64Data:
		h, err := cdf.ReadHeader(r, size)
		if err != nil {
			return nil, translate(err)
		}
		return buildFile(r, size, closer, format, h)
	case FormatHDF5:
		return nil, fmt.Errorf("%w: NetCDF-4 stores its data in an HDF5 file", ErrUnsupportedFormat)
	default:
		return nil, ErrNotNetCDF
	}
}

func buildFile(r io.ReaderAt, size int64, closer io.Closer, format Format, h *cdf.Header) (*File, error) {
	f := &File{
		r:          r,
		size:       size,
		closer:     closer,
		h:          h,
		format:     format,
		dimByName:  make(map[string]*Dimension, len(h.Dims)),
		attrByName: make(map[string]*Attribute, len(h.Attrs)),
		varByName:  make(map[string]*Variable, len(h.Vars)),
	}
	f.dims = make([]*Dimension, len(h.Dims))
	for i, d := range h.Dims {
		dim := &Dimension{name: d.Name, size: d.Size, file: f}
		f.dims[i] = dim
		f.dimByName[d.Name] = dim
	}
	f.attrs = make([]*Attribute, len(h.Attrs))
	for i, a := range h.Attrs {
		attr := newAttribute(a)
		f.attrs[i] = attr
		f.attrByName[a.Name] = attr
	}
	f.vars = make([]*Variable, len(h.Vars))
	for i := range h.Vars {
		v, err := newVariable(f, &h.Vars[i])
		if err != nil {
			return nil, err
		}
		f.vars[i] = v
		f.varByName[v.v.Name] = v
	}
	return f, nil
}

// Close releases the file. It is idempotent; for a File created with
// NewReader it does not close the caller's reader.
func (f *File) Close() error {
	if f.closed.Swap(true) {
		return nil
	}
	if f.closer != nil {
		return f.closer.Close()
	}
	return nil
}

// Format returns the file's format.
func (f *File) Format() Format { return f.format }

// Dimensions returns the file's dimensions in definition order.
func (f *File) Dimensions() []*Dimension {
	return append([]*Dimension(nil), f.dims...)
}

// Dimension returns the dimension with the given name.
func (f *File) Dimension(name string) (*Dimension, bool) {
	d, ok := f.dimByName[name]
	return d, ok
}

// Attributes returns the global attributes in definition order.
func (f *File) Attributes() []*Attribute {
	return append([]*Attribute(nil), f.attrs...)
}

// Attribute returns the global attribute with the given name.
func (f *File) Attribute(name string) (*Attribute, bool) {
	a, ok := f.attrByName[name]
	return a, ok
}

// Variables returns the file's variables in definition order.
func (f *File) Variables() []*Variable {
	return append([]*Variable(nil), f.vars...)
}

// Variable returns the variable with the given name.
func (f *File) Variable(name string) (*Variable, bool) {
	v, ok := f.varByName[name]
	return v, ok
}

func (f *File) checkOpen() error {
	if f.closed.Load() {
		return ErrClosed
	}
	return nil
}
