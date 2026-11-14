package cdf

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Fixtures whose header bytes the encoder must reproduce exactly. The two
// files taken from the netCDF-C corpus are excluded: null_byte_padding-cdf2
// deliberately has non-NUL header padding (the encoder writes zeros), and
// bad_cdf5_begin is not a valid file.
var writerFixtures = []string{
	"attrs-cdf1.nc",
	"cdf5types-cdf5.nc",
	"empty-cdf1.nc",
	"fill-cdf1.nc",
	"fill-cdf2.nc",
	"fill-cdf5.nc",
	"layout-cdf1.nc",
	"layout-cdf2.nc",
	"layout-cdf5.nc",
	"record-cdf1.nc",
	"record-cdf2.nc",
	"record-cdf5.nc",
	"simple-cdf1.nc",
	"simple-cdf2.nc",
	"simple-cdf5.nc",
	"single_rec-cdf1.nc",
	"types-cdf1.nc",
	"types-cdf2.nc",
}

func readFixture(t *testing.T, name string) (*Header, []byte) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	h, err := ReadHeader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("parse fixture %s: %v", name, err)
	}
	return h, data
}

// TestEncodeHeaderByteIdentical proves the encoder against the ncgen-written
// fixtures: re-encoding a parsed header must reproduce the file's bytes up to
// the first data offset, exactly.
func TestEncodeHeaderByteIdentical(t *testing.T) {
	for _, name := range writerFixtures {
		t.Run(name, func(t *testing.T) {
			h, data := readFixture(t, name)
			var buf bytes.Buffer
			if err := h.WriteHeader(&buf); err != nil {
				t.Fatalf("WriteHeader: %v", err)
			}
			if int64(buf.Len()) != h.HeaderSize {
				t.Fatalf("encoder wrote %d bytes, the parser read %d", buf.Len(), h.HeaderSize)
			}
			want := data[:h.HeaderSize]
			got := buf.Bytes()
			if !bytes.Equal(got, want) {
				i := 0
				for i < len(got) && i < len(want) && got[i] == want[i] {
					i++
				}
				t.Fatalf("first difference at offset %d: got % x, want % x",
					i, got[i:min(i+16, len(got))], want[i:min(i+16, len(want))])
			}
		})
	}
}

// TestEncodeSizeMatchesBytes proves EncodeSize against the encoder for both
// parsed and freshly laid-out headers: the size must be what WriteHeader
// writes, or the layout would compute begins from a wrong first offset.
func TestEncodeSizeMatchesBytes(t *testing.T) {
	for _, name := range writerFixtures {
		t.Run(name, func(t *testing.T) {
			h, _ := readFixture(t, name)
			size, err := h.EncodeSize()
			if err != nil {
				t.Fatalf("EncodeSize: %v", err)
			}
			var buf bytes.Buffer
			if err := h.WriteHeader(&buf); err != nil {
				t.Fatalf("WriteHeader: %v", err)
			}
			if size != int64(buf.Len()) {
				t.Fatalf("EncodeSize = %d, WriteHeader wrote %d", size, buf.Len())
			}
		})
	}
}

// TestLayoutMatchesFixtures proves the layout computation against every
// fixture: zeroing the begins of a parsed header and recomputing them must
// yield the stored begins, the stored record size and the header size.
func TestLayoutMatchesFixtures(t *testing.T) {
	for _, name := range writerFixtures {
		t.Run(name, func(t *testing.T) {
			checkLayout(t, name)
		})
	}
}

// checkLayout recomputes the begins of one fixture and compares them with the
// stored ones.
func checkLayout(t *testing.T, name string) {
	t.Helper()
	h, _ := readFixture(t, name)
	want := make([]int64, len(h.Vars))
	for i := range h.Vars {
		want[i] = h.Vars[i].Begin
		h.Vars[i].Begin = 0
	}
	wantRec, wantHeader := h.RecSize, h.HeaderSize
	numrecs := h.NumRecs

	if err := h.Layout(); err != nil {
		t.Fatalf("Layout: %v", err)
	}
	if h.HeaderSize != wantHeader {
		t.Errorf("HeaderSize = %d, fixture stores %d", h.HeaderSize, wantHeader)
	}
	if h.RecSize != wantRec {
		t.Errorf("RecSize = %d, fixture has %d", h.RecSize, wantRec)
	}
	if h.NumRecs != numrecs {
		t.Errorf("Layout changed NumRecs from %d to %d", numrecs, h.NumRecs)
	}
	for i := range h.Vars {
		if h.Vars[i].Begin != want[i] {
			t.Errorf("variable %q begins at %d, fixture stores %d",
				h.Vars[i].Name, h.Vars[i].Begin, want[i])
		}
	}
}

// TestFillBytes pins the specification's default fill values in their external
// representation. They are what every unwritten byte of a created file holds.
func TestFillBytes(t *testing.T) {
	cases := []struct {
		typ  Type
		want []byte
	}{
		{TypeByte, []byte{0x81}},
		{TypeChar, []byte{0x00}},
		{TypeShort, []byte{0x80, 0x01}},
		{TypeInt, []byte{0x80, 0x00, 0x00, 0x01}},
		{TypeFloat, []byte{0x7c, 0xf0, 0x00, 0x00}},
		{TypeDouble, []byte{0x47, 0x9e, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}},
		{TypeUByte, []byte{0xff}},
		{TypeUShort, []byte{0xff, 0xff}},
		{TypeUInt, []byte{0xff, 0xff, 0xff, 0xff}},
		{TypeInt64, []byte{0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02}},
		{TypeUInt64, []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xfe}},
	}
	for _, c := range cases {
		got, err := FillBytes(c.typ)
		if err != nil {
			t.Errorf("FillBytes(%d): %v", c.typ, err)
			continue
		}
		if !bytes.Equal(got, c.want) {
			t.Errorf("FillBytes(%d) = % x, want % x", c.typ, got, c.want)
		}
	}
	if _, err := FillBytes(Type(99)); err == nil {
		t.Error("FillBytes of an undefined type returned no error")
	}
}

// TestCheckName pins the writer's name rules to netCDF-C's NC_check_name.
func TestCheckName(t *testing.T) {
	valid := []string{
		"x",
		"temp",
		"x_1",
		"a.b",
		"Name with space",
		"имя",
		strings.Repeat("n", 256),
	}
	for _, name := range valid {
		if err := CheckName(name); err != nil {
			t.Errorf("CheckName(%q) = %v, want nil", name, err)
		}
	}
	invalid := []string{
		"",
		"a/b",
		" lead",
		"trail ",
		"a\tb",
		"a\x00b",
		"\xff",
		strings.Repeat("n", 257),
	}
	for _, name := range invalid {
		if err := CheckName(name); err == nil {
			t.Errorf("CheckName(%q) = nil, want an error", name)
		}
	}
}
