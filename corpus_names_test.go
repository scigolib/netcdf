package netcdf_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/scigolib/netcdf"
)

// Two of netCDF-C's nctest reference files store names whose declared length
// runs past the name into NUL bytes: ncdump prints them as "yy", "pp" and
// "plugh". The reader ends a name at its first NUL, as C does, so lookup sees
// the name a user would type and no name carries a NUL.
func TestNULPaddedNames(t *testing.T) {
	for _, name := range []string{"nctest_names-cdf1.nc", "nctest_names-cdf2.nc"} {
		t.Run(name, func(t *testing.T) {
			f := openFixture(t, name)
			checkTrimmedNamesFound(t, f)
			checkNoNULInNames(t, f)
		})
	}
}

func checkTrimmedNamesFound(t *testing.T, f *netcdf.File) {
	t.Helper()
	if _, ok := f.Variable("yy"); !ok {
		t.Error(`Variable("yy") not found`)
	}
	if _, ok := f.Dimension("pp"); !ok {
		t.Error(`Dimension("pp") not found`)
	}
	for _, a := range f.Attributes() {
		if a.Name() == "plugh" {
			return
		}
	}
	t.Error(`global attribute "plugh" not found`)
}

func checkNoNULInNames(t *testing.T, f *netcdf.File) {
	t.Helper()
	check := func(what, name string) {
		if strings.ContainsRune(name, 0) {
			t.Errorf("%s name %q holds a NUL", what, name)
		}
	}
	for _, d := range f.Dimensions() {
		check("dimension", d.Name())
	}
	for _, a := range f.Attributes() {
		check("global attribute", a.Name())
	}
	for _, v := range f.Variables() {
		check("variable", v.Name())
		what := "variable " + strconv.Quote(v.Name()) + " attribute"
		for _, a := range v.Attributes() {
			check(what, a.Name())
		}
	}
}
