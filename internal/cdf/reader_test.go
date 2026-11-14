package cdf

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

// The three overflow detectors guard every shape and offset computation but
// cannot fire on a file small enough for a test to build, so they are checked
// directly.

func TestMulCheck(t *testing.T) {
	if _, err := mulCheck(-1, 2); err == nil {
		t.Error("mulCheck(-1, 2): want an error")
	}
	if _, err := mulCheck(2, math.MaxInt64/2+1); err == nil {
		t.Error("mulCheck(2, MaxInt64/2+1): want an overflow error")
	}
	if got, err := mulCheck(math.MaxInt64/2, 2); err != nil || got != math.MaxInt64-1 {
		t.Errorf("mulCheck(MaxInt64/2, 2) = %d, %v; want %d, nil", got, err, int64(math.MaxInt64-1))
	}
}

func TestAddCheck(t *testing.T) {
	if _, err := addCheck(-1, 2); err == nil {
		t.Error("addCheck(-1, 2): want an error")
	}
	if _, err := addCheck(math.MaxInt64, 1); err == nil {
		t.Error("addCheck(MaxInt64, 1): want an overflow error")
	}
	if got, err := addCheck(math.MaxInt64-1, 1); err != nil || got != math.MaxInt64 {
		t.Errorf("addCheck(MaxInt64-1, 1) = %d, %v; want MaxInt64, nil", got, err)
	}
}

func TestRoundUp4(t *testing.T) {
	if _, err := roundUp4(-1); err == nil {
		t.Error("roundUp4(-1): want an error")
	}
	if _, err := roundUp4(math.MaxInt64); err == nil {
		t.Error("roundUp4(MaxInt64): want an overflow error")
	}
	if got, err := roundUp4(5); err != nil || got != 8 {
		t.Errorf("roundUp4(5) = %d, %v; want 8, nil", got, err)
	}
}

// TestNameStopsAtFirstNUL builds a name whose declared length runs past the
// name into NUL bytes and checks that the cursor returns the name without
// them, as netCDF-C does.
func TestNameStopsAtFirstNUL(t *testing.T) {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint32(buf[0:], 3)
	copy(buf[4:], []byte{'y', 'y', 0, 0})
	c := newCursor(bytes.NewReader(buf), int64(len(buf)))
	got, err := c.name(Version1)
	if err != nil {
		t.Fatalf("name: %v", err)
	}
	if got != "yy" {
		t.Fatalf("name = %q, want %q", got, "yy")
	}
}
