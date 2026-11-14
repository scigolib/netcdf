package netcdf

import (
	"errors"
	"fmt"

	"github.com/scigolib/netcdf/internal/cdf"
)

// Errors reported by the package; all are matchable with errors.Is.
var (
	// ErrNotNetCDF reports input that is not a NetCDF file.
	ErrNotNetCDF = errors.New("not a NetCDF file")

	// ErrCorrupt reports a file whose header or data violates the format.
	ErrCorrupt = errors.New("corrupt NetCDF file")

	// ErrOutOfRange reports an index or selection outside the variable, or a
	// destination slice whose length does not match the selection.
	ErrOutOfRange = errors.New("selection outside the variable")

	// ErrUnsupportedFormat reports a NetCDF format this package does not read
	// yet: a NetCDF-4 file (HDF5-backed) is detected and reported with it.
	ErrUnsupportedFormat = errors.New("unsupported netCDF format")

	// ErrTypeMismatch reports a destination slice whose element type does not
	// match the variable's netCDF type.
	ErrTypeMismatch = errors.New("destination type does not match the data type")

	// ErrClosed reports use of a File after Close.
	ErrClosed = errors.New("netCDF file is closed")
)

// translate maps the format layer's error kinds to this package's public
// sentinels, keeping the cause's detail (ADR-003). Errors this package creates
// itself pass through unchanged.
func translate(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, cdf.ErrNotNetCDF):
		return fmt.Errorf("%w: %s", ErrNotNetCDF, err.Error())
	case errors.Is(err, cdf.ErrOutOfRange):
		return fmt.Errorf("%w: %s", ErrOutOfRange, err.Error())
	case errors.Is(err, cdf.ErrCorrupt):
		return fmt.Errorf("%w: %s", ErrCorrupt, err.Error())
	}
	return err
}
