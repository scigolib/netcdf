# netcdf

[![CI](https://github.com/scigolib/netcdf/actions/workflows/ci.yml/badge.svg)](https://github.com/scigolib/netcdf/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/scigolib/netcdf.svg)](https://pkg.go.dev/github.com/scigolib/netcdf)
[![codecov](https://codecov.io/gh/scigolib/netcdf/branch/main/graph/badge.svg)](https://codecov.io/gh/scigolib/netcdf)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

Pure Go reader and writer for NetCDF files — classic CDF-1, CDF-2, CDF-5 and NetCDF-4 — with
no cgo and no C dependencies. It is part of the [scigolib](https://github.com/scigolib) ecosystem
and builds NetCDF-4 on top of [scigolib/hdf5](https://github.com/scigolib/hdf5).

**Status: early development.** The classic reader (CDF-1, CDF-2, CDF-5) reads headers, attributes,
metadata and data. The writer and NetCDF-4 support are next. The API is not stable before v1.0.

## Install

```bash
go get github.com/scigolib/netcdf
```

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

## Formats

| Format | Starts with | Read | Write |
| ------ | ----------- | ---- | ----- |
| CDF-1 (classic) | `CDF` + version 1 | yes | planned |
| CDF-2 (64-bit offset) | `CDF` + version 2 | yes | planned |
| CDF-5 (64-bit data) | `CDF` + version 5 | yes | planned |
| NetCDF-4 (HDF5) | the eight-byte HDF5 signature | planned | planned |

## Why another NetCDF library

- **Pure Go, zero cgo.** Cross-compiles to every platform Go supports, no C toolchain.
- **One engine for both worlds.** The HDF5 layer behind NetCDF-4 is
  [scigolib/hdf5](https://github.com/scigolib/hdf5), ours; classic and NetCDF-4 files share one
  codebase and one test matrix.
- **Checked against the C implementation.** Files we write are read back by `ncdump` and
  `nccopy`; files they write are read by us.

## Documentation

- [NetCDF file format specifications](https://docs.unidata.ucar.edu/netcdf-c/current/file_format_specifications.html)
- [netCDF-C](https://github.com/Unidata/netcdf-c) — the reference implementation
- [scigolib/hdf5](https://github.com/scigolib/hdf5) — the HDF5 engine behind NetCDF-4
- [AGENTS.md](AGENTS.md) — instructions for AI agents working with the library

## License

MIT — see [LICENSE](LICENSE).
