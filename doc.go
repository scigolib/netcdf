// Package netcdf reads and writes NetCDF files.
//
// The classic formats — CDF-1, CDF-2 (64-bit offset) and CDF-5 (64-bit data) —
// are read and written today. NetCDF-4 stores its data in an HDF5 file and is
// planned on top of github.com/scigolib/hdf5. The classic formats are defined
// by the NetCDF Classic Format Specification
// (https://docs.unidata.ucar.edu/netcdf-c/current/file_format_specifications.html);
// the reference implementation is netCDF-C (https://github.com/Unidata/netcdf-c).
//
// The classic formats are big-endian: every value is stored in network byte
// order, whatever the host order. The package spells that out at each decode
// site and never assumes the native order.
//
// Data access is typed. Variable.Read, Variable.ReadSlice, VarWriter.Write and
// VarWriter.WriteSlice are generic over the element type, which is inferred
// from the argument ([]int8 for byte, []byte for char and ubyte, and the
// matching slice type for the rest), so the element type is checked at compile
// time. A file's type is known only after opening it, so the variable's type is
// also checked at run time; a mismatch is ErrTypeMismatch, never a silent
// conversion. When the type is discovered at run time, switch on Variable.Type
// and call the typed method per arm (see ExampleVariable_Read_dynamic).
//
// The generic methods live on the concrete Variable and VarWriter types: Go
// does not allow generic interface methods, so an interface-based abstraction
// over them would need its own non-generic contract.
//
// Status: early development. The classic formats are read and written —
// header, dimensions, attributes, variables and data (whole variables and
// hyperslabs). NetCDF-4 is not implemented yet, and the API is not stable
// before v1.0.
package netcdf
