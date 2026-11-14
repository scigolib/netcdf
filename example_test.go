package netcdf_test

import (
	"fmt"

	"github.com/scigolib/netcdf"
)

func ExampleDetectFormat() {
	// The first eight bytes of a file are enough to tell the formats apart.
	header := []byte("CDF\x02rest of the header")

	fmt.Println(netcdf.DetectFormat(header))

	// Output: CDF-2
}
