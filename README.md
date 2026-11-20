# netcdf

[![CI](https://github.com/scigolib/netcdf/actions/workflows/ci.yml/badge.svg)](https://github.com/scigolib/netcdf/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/scigolib/netcdf.svg)](https://pkg.go.dev/github.com/scigolib/netcdf)
[![codecov](https://codecov.io/gh/scigolib/netcdf/branch/main/graph/badge.svg)](https://codecov.io/gh/scigolib/netcdf)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

Pure Go reader and writer for NetCDF files — classic CDF-1, CDF-2, CDF-5 and NetCDF-4 — with
no cgo and no C dependencies. It is part of the [scigolib](https://github.com/scigolib) ecosystem
and builds NetCDF-4 on top of [scigolib/hdf5](https://github.com/scigolib/hdf5).

**Status: early development.** The classic formats (CDF-1, CDF-2, CDF-5) are read and written:
headers, attributes, metadata and data, whole variables and hyperslabs. NetCDF-4 support is
next. The API is not stable before v1.0.

## Install

```bash
go get github.com/scigolib/netcdf
```

Requires Go 1.27 or newer. We support only Go releases that still receive security fixes
(1.26 and 1.27 today). Go 1.27 also enables generic methods, which let one type-safe method
replace a family of per-type methods — so the API stays small. This library does not adopt
them: its typed helpers are generic functions, and a generic method would be added only
where it collapses a real family.

## Quick start

```go
package main

import (
	"fmt"
	"log"

	"github.com/scigolib/netcdf"
)

func main() {
	f, err := netcdf.Open("data.nc")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	// Dimensions, attributes and variables are parsed when the file opens;
	// data is read on demand.
	for _, v := range f.Variables() {
		fmt.Println(v.Name(), v.Type(), v.Shape())
	}

	temp, ok := f.Variable("temp")
	if !ok {
		log.Fatal("no variable temp")
	}
	values := make([]float64, temp.Len())
	if err := temp.Read(&values); err != nil {
		log.Fatal(err)
	}
	fmt.Println(values)

	// A hyperslab: the first two records of the first column.
	part := make([]float64, 2)
	if err := temp.ReadSlice(&part, []int64{0, 0}, []int64{2, 1}); err != nil {
		log.Fatal(err)
	}
	fmt.Println(part)
}
```

`Read` and `ReadSlice` take a pointer to a slice whose element type matches the variable's
netCDF type: `[]int8` for byte, `[]byte` for char and ubyte, `[]int16`, `[]uint16`, `[]int32`,
`[]uint32`, `[]float32`, `[]float64`, `[]int64`, `[]uint64`. A mismatched slice is reported with
`ErrTypeMismatch`, never silently converted.

## Writing a file

```go
schema := netcdf.NewSchema(netcdf.FormatClassic) // CDF-1; FormatClassic64 is CDF-2, FormatClassic64Data is CDF-5
time := schema.AddDimension("time", 0)           // length 0 = unlimited
x := schema.AddDimension("x", 3)
temp := schema.AddVariable("temp", netcdf.TypeFloat, time, x)
if err := temp.SetAttr("units", "K"); err != nil {
	log.Fatal(err)
}

w, err := netcdf.Create("out.nc", schema)
if err != nil {
	log.Fatal(err)
}
v, _ := w.Variable("temp")
// Two records of three values; writing past the current count appends and
// fills the records in between with the fill value.
if err := v.WriteSlice([]float32{1, 2, 3, 4, 5, 6}, []int64{0, 0}, []int64{2, 3}); err != nil {
	log.Fatal(err)
}
if err := w.Close(); err != nil { // writes the record count; the file is consistent
	log.Fatal(err)
}
```

Attribute values are Go slices whose netCDF type is inferred (`string`/`[]byte` are char,
`[]int8` byte, `[]int32` int, `[]float64` double, …); `SetAttrTyped` states the type explicitly
for the ambiguous cases (ubyte vs char, uint vs int). `Create` validates the whole schema before
touching the file; a schema error is reported before anything is written. The output is
byte-identical to netCDF-C's `ncgen` for the same model and format (the fixtures in `testdata/`
are the proof). `ncdump` and `nccopy` read it too — checked by hand against netCDF-C 4.10.1 on
2026-11-14; automated differential runs against the C tools are planned.

## Formats

| Format | Starts with | Read | Write |
| ------ | ----------- | ---- | ----- |
| CDF-1 (classic) | `CDF` + version 1 | yes | yes |
| CDF-2 (64-bit offset) | `CDF` + version 2 | yes | yes |
| CDF-5 (64-bit data) | `CDF` + version 5 | yes | yes |
| NetCDF-4 (HDF5) | the eight-byte HDF5 signature | planned | planned |

## Why another NetCDF library

- **Pure Go, zero cgo.** Cross-compiles to every platform Go supports, no C toolchain.
- **One ecosystem.** NetCDF-4 will sit on [scigolib/hdf5](https://github.com/scigolib/hdf5),
  our own HDF5 engine; the classic formats implemented today need no other library.
- **Checked against the C implementation.** We read files that `ncgen` writes, and the files
  we write have been read back by `ncdump` and `nccopy` by hand (netCDF-C 4.10.1, 2026-11-14).

## Documentation

- [NetCDF file format specifications](https://docs.unidata.ucar.edu/netcdf-c/current/file_format_specifications.html)
- [netCDF-C](https://github.com/Unidata/netcdf-c) — the reference implementation
- [scigolib/hdf5](https://github.com/scigolib/hdf5) — the HDF5 engine behind NetCDF-4
- [AGENTS.md](AGENTS.md) — instructions for AI agents working with the library

## License

MIT — see [LICENSE](LICENSE).
