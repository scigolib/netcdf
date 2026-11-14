package netcdf

import "fmt"

// Format identifies a NetCDF file format.
type Format int

const (
	// FormatUnknown means the header matches no NetCDF format.
	FormatUnknown Format = iota

	// FormatClassic is CDF-1, the classic format with 32-bit offsets.
	FormatClassic

	// FormatClassic64 is CDF-2, the classic format with 64-bit offsets.
	FormatClassic64

	// FormatClassic64Data is CDF-5, the classic format with 64-bit offsets
	// and 64-bit sizes.
	FormatClassic64Data

	// FormatHDF5 is NetCDF-4, which stores its data in an HDF5 file.
	FormatHDF5
)

// String returns the format's name as the NetCDF documentation writes it.
func (f Format) String() string {
	switch f {
	case FormatClassic:
		return "CDF-1"
	case FormatClassic64:
		return "CDF-2"
	case FormatClassic64Data:
		return "CDF-5"
	case FormatHDF5:
		return "NetCDF-4"
	case FormatUnknown:
		return "unknown"
	default:
		return fmt.Sprintf("Format(%d)", int(f))
	}
}

const (
	// classicMagic starts every classic NetCDF file.
	classicMagic = "CDF"

	// classicHeaderLen is the magic plus the version byte.
	classicHeaderLen = 4

	// hdf5Signature is the eight-byte signature every HDF5 file starts with.
	hdf5Signature = "\x89HDF\r\n\x1a\n"
)

// DetectFormat reports the format of a NetCDF file from its first bytes.
//
// header must hold at least the first eight bytes of the file. A shorter slice
// is not an error: it simply cannot match the HDF5 signature and yields
// FormatUnknown unless it carries a complete classic header.
//
// A classic file starts with the three bytes "CDF" and a version byte: 1 is
// CDF-1, 2 is CDF-2 and 5 is CDF-5. Version bytes 3 and 4 were proposed and
// never used; they are reported as FormatUnknown. A NetCDF-4 file is an HDF5
// file written by netCDF-C without a user block, so the HDF5 signature sits at
// offset 0.
func DetectFormat(header []byte) Format {
	if len(header) >= classicHeaderLen && string(header[:len(classicMagic)]) == classicMagic {
		switch header[3] {
		case 1:
			return FormatClassic
		case 2:
			return FormatClassic64
		case 5:
			return FormatClassic64Data
		}
		return FormatUnknown
	}

	if len(header) >= len(hdf5Signature) && string(header[:len(hdf5Signature)]) == hdf5Signature {
		return FormatHDF5
	}

	return FormatUnknown
}
