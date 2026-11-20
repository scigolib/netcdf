//go:build oracle

package netcdf_test

import (
	"os"
	"path/filepath"
	"testing"
)

// oracleCorpus is the classic-expressible subset of netCDF-C's nc_test CDL
// models at the pin. Provenance, hashes and the two documented exclusions are
// in testdata/README.md. Every model must generate for every listed kind: a
// model that stops generating is a finding, never a skip.
var oracleCorpus = []struct {
	name  string
	kinds []string
}{
	{"nc_enddef", []string{"1", "2", "5"}},
	{"nc_sync", []string{"1", "2", "5"}},
	{"ref_tst_diskless3_create", []string{"1", "2", "5"}},
	{"ref_tst_diskless3_open", []string{"1", "2", "5"}},
	{"ref_tst_http_nc3", []string{"1", "2", "5"}},
	{"ref_tst_http_nc4a", []string{"1", "2", "5"}},
	{"ref_tst_http_nc4c", []string{"1", "2", "5"}},
	{"ref_tst_http_nc4f", []string{"1", "2", "5"}},
	{"tst_diskless5", []string{"1", "2", "5"}},
}

// TestOracleToolsPresent fails when a reference tool is missing: a run with the
// oracle tag must never pass by skipping.
func TestOracleToolsPresent(t *testing.T) {
	for _, tool := range []string{"ncgen", "ncdump", "nccopy"} {
		t.Logf("%s: %s", tool, oracleTool(t, tool))
	}
}

// TestOracleCorpus feeds every corpus model through the reference generator,
// our reader, our writer and the reference reader again; the reference reader's
// text over our file must equal its text over the generated file. nccopy then
// converts both to another kind and the converted texts are compared too.
func TestOracleCorpus(t *testing.T) {
	ncgen := oracleTool(t, "ncgen")
	ncdump := oracleTool(t, "ncdump")
	nccopy := oracleTool(t, "nccopy")
	for _, model := range oracleCorpus {
		for _, kind := range model.kinds {
			model, kind := model, kind
			t.Run(model.name+"/cdf"+kind, func(t *testing.T) {
				oracleModel(t, ncgen, ncdump, nccopy, model.name, kind)
			})
		}
	}
}

func oracleModel(t *testing.T, ncgen, ncdump, nccopy, model, kind string) {
	t.Helper()
	dir := t.TempDir()
	cdl := filepath.Join("testdata", "oracle", "cdl", model+".cdl")
	if _, err := os.Stat(cdl); err != nil {
		t.Fatalf("corpus model %s: %v", cdl, err)
	}
	ref := filepath.Join(dir, "ref.nc")
	runOracle(t, ncgen, "-k", kind, "-o", ref, cdl)

	ours := filepath.Join(dir, "ours.nc")
	writeThroughOurAPI(t, ref, ours)

	// The same dataset name and full precision on both sides: the only thing
	// that may differ is what the two files contain.
	dump := func(path string) string {
		return runOracle(t, ncdump, "-n", model, "-p", "17,17", path)
	}
	if diff := firstDifference(dump(ref), dump(ours)); diff != "" {
		t.Errorf("our file differs from the ncgen reference through ncdump: %s", diff)
	}

	// nccopy must read what we write and preserve the content it converts.
	other := otherKind(kind)
	convRef := filepath.Join(dir, "ref-cdf"+other+".nc")
	convOurs := filepath.Join(dir, "ours-cdf"+other+".nc")
	runOracle(t, nccopy, "-k", other, ref, convRef)
	runOracle(t, nccopy, "-k", other, ours, convOurs)
	if diff := firstDifference(dump(convRef), dump(convOurs)); diff != "" {
		t.Errorf("our file after nccopy -k %s differs from the reference: %s", other, diff)
	}
}

// otherKind returns a kind different from kind, so nccopy has to convert.
func otherKind(kind string) string {
	switch kind {
	case "1":
		return "2"
	case "2":
		return "5"
	default:
		return "1"
	}
}
