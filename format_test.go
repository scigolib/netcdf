package netcdf

import "testing"

func TestDetectFormat(t *testing.T) {
	tests := []struct {
		name   string
		header []byte
		want   Format
	}{
		{"CDF-1", []byte("CDF\x01"), FormatClassic},
		{"CDF-2", []byte("CDF\x02rest of the header"), FormatClassic64},
		{"CDF-5", []byte("CDF\x05rest of the header"), FormatClassic64Data},
		{"CDF-1 full header", []byte("CDF\x01\x00\x00\x00\x00\x00\x0a"), FormatClassic},
		{"HDF5 signature", []byte("\x89HDF\r\n\x1a\nrest"), FormatHDF5},
		{"HDF5 signature only", []byte(hdf5Signature), FormatHDF5},
		{"empty", nil, FormatUnknown},
		{"three bytes of magic", []byte("CDF"), FormatUnknown},
		{"classic version 3 was never used", []byte("CDF\x03"), FormatUnknown},
		{"classic version 4 was never used", []byte("CDF\x04"), FormatUnknown},
		{"first byte differs", []byte("NDF\x01"), FormatUnknown},
		{"seven bytes of the HDF5 signature", []byte("\x89HDF\r\n\x1a"), FormatUnknown},
		{"other file", []byte("PK\x03\x04rest"), FormatUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DetectFormat(tt.header); got != tt.want {
				t.Errorf("DetectFormat(%q) = %v, want %v", tt.header, got, tt.want)
			}
		})
	}
}

func TestFormatString(t *testing.T) {
	tests := []struct {
		format Format
		want   string
	}{
		{FormatClassic, "CDF-1"},
		{FormatClassic64, "CDF-2"},
		{FormatClassic64Data, "CDF-5"},
		{FormatHDF5, "NetCDF-4"},
		{FormatUnknown, "unknown"},
		{Format(42), "Format(42)"},
	}

	for _, tt := range tests {
		if got := tt.format.String(); got != tt.want {
			t.Errorf("Format(%d).String() = %q, want %q", int(tt.format), got, tt.want)
		}
	}
}
