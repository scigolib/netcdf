# AGENTS.md — netcdf

> NetCDF files in pure Go: classic CDF-1/2/5 and NetCDF-4, no cgo.

## What is netcdf

`netcdf` reads and writes NetCDF files in pure Go. The classic formats (CDF-1, CDF-2, CDF-5)
are big-endian binary formats defined by the NetCDF Classic Format Specification; NetCDF-4 is
an HDF5 file with NetCDF conventions on top, and this library uses
[scigolib/hdf5](https://github.com/scigolib/hdf5) for that layer.

Status: early development. `DetectFormat` is the only public function today; the API is not
stable before v1.0.

## Quick Start

```go
header := make([]byte, 8)
if _, err := io.ReadFull(f, header); err != nil {
	return err
}
switch netcdf.DetectFormat(header) {
case netcdf.FormatClassic:
	// CDF-1
case netcdf.FormatHDF5:
	// NetCDF-4
}
```

## Build & Test

```bash
go build ./...
go test ./...
go test -race ./...        # needs cgo: Linux, macOS, or WSL on Windows
make ci                    # the gates CI runs: fmt, build, test, lint
```

## Architecture

```
netcdf/
├── doc.go       package overview, format specifications, status
├── format.go    Format + DetectFormat: magic and version byte, HDF5 signature
└── internal/    (to come) classic reader/writer, NetCDF-4 conventions on scigolib/hdf5
```

Planned boundaries: the classic parser works on `io.ReaderAt` with an explicit big-endian
order; NetCDF-4 delegates to `scigolib/hdf5`; the public API is format-agnostic.

## Rules for agents

- The classic formats are big-endian. Decode with an explicit `binary.ByteOrder`, never the
  host order.
- Validate every declared length and offset against the input size before allocating.
- A bug fix starts with a test that fails on `main`; a parser change cites the spec section or
  the `ncdump`/`nccopy` output it is checked against.

## Links

- [NetCDF Classic format specifications](https://docs.unidata.ucar.edu/netcdf-c/current/file_format_specifications.html)
- [netCDF-C](https://github.com/Unidata/netcdf-c) — reference implementation
- [scigolib/hdf5](https://github.com/scigolib/hdf5) — HDF5 engine for NetCDF-4
