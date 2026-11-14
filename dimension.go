package netcdf

// Dimension is one dimension of a NetCDF file. Dimensions are shared by every
// variable that uses them.
type Dimension struct {
	name string
	size int64 // 0 marks the unlimited dimension
	file *File
}

// Name returns the dimension's name.
func (d *Dimension) Name() string { return d.name }

// Len returns the dimension's current length. For the unlimited dimension
// that is the file's record count, not the zero the header stores.
func (d *Dimension) Len() int64 {
	if d.size == 0 {
		return d.file.h.NumRecs
	}
	return d.size
}

// Unlimited reports whether the dimension is the record (unlimited) dimension.
func (d *Dimension) Unlimited() bool { return d.size == 0 }
