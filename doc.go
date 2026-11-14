// Package netcdf reads and writes NetCDF files.
//
// It supports the classic formats — CDF-1, CDF-2 (64-bit offset) and CDF-5
// (64-bit data) — and NetCDF-4, which stores its data in an HDF5 file. The
// classic formats are defined by the NetCDF Classic Format Specification
// (https://docs.unidata.ucar.edu/netcdf-c/current/file_format_specifications.html);
// the reference implementation is netCDF-C (https://github.com/Unidata/netcdf-c).
// The HDF5 layer of NetCDF-4 comes from github.com/scigolib/hdf5.
//
// The classic formats are big-endian: every value is stored in network byte
// order, whatever the host order. The package spells that out at each decode
// site and never assumes the native order.
//
// Status: early development. The classic formats are read and written —
// header, dimensions, attributes, variables and data (whole variables and
// hyperslabs); NetCDF-4 support is being built, and the API is not stable
// before v1.0.
package netcdf
