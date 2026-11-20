//go:build ignore

// Regenerates testdata/cdf5types-cdf5.nc: the CDF-5 file with the five types
// the classic-only ncgen cannot produce. The file is the writer's own output;
// ncdump reading it (testdata/ncdump/cdf5types-cdf5.dump.txt) is the oracle
// evidence, and TestCreateMatchesFixtures holds it byte for byte so a change
// in the CDF-5 encoding fails the suite.
//
// Usage, from the repository root:
//
//	go run testdata/gen/cdf5types.go testdata/cdf5types-cdf5.nc
package main

import (
	"fmt"
	"os"

	"github.com/scigolib/netcdf"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run testdata/gen/cdf5types.go <out.nc>")
		os.Exit(2)
	}
	s := netcdf.NewSchema(netcdf.FormatClassic64Data)
	n := s.AddDimension("n", 3)
	s.AddVariable("ub", netcdf.TypeUByte, n)
	s.AddVariable("us", netcdf.TypeUShort, n)
	s.AddVariable("ui", netcdf.TypeUInt, n)
	s.AddVariable("i64", netcdf.TypeInt64, n)
	s.AddVariable("u64", netcdf.TypeUInt64, n)

	w, err := netcdf.Create(os.Args[1], s)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	write := func(name string, values any) {
		v, ok := w.Variable(name)
		if !ok {
			panic("missing variable " + name)
		}
		var err error
		switch vals := values.(type) {
		case []byte:
			err = v.Write(vals)
		case []uint16:
			err = v.Write(vals)
		case []uint32:
			err = v.Write(vals)
		case []int64:
			err = v.Write(vals)
		case []uint64:
			err = v.Write(vals)
		default:
			panic("unsupported values type")
		}
		if err != nil {
			panic(err)
		}
	}
	write("ub", []byte{0, 128, 255})
	write("us", []uint16{0, 32768, 65534})
	write("ui", []uint32{0, 2147483648, 4294967294})
	write("i64", []int64{-9223372036854775807, 0, 9223372036854775807})
	write("u64", []uint64{0, 9223372036854775808, 18446744073709551615})
	if err := w.Close(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
