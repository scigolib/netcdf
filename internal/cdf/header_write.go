package cdf

// The writer's format layer: header encoding, layout computation and fill.
// It is the mirror of header.go/reader.go: one model, one grammar, two
// directions. Everything here is byte-level format knowledge; the public
// package only builds a Header and calls these functions.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"unicode/utf8"
)

// EncodeSize returns the number of bytes WriteHeader writes for h.
//
// The header's length does not depend on the variables' starting offsets:
// every field has a width fixed by the format version, so a header can be
// sized before any begin is assigned (NC_begins relies on the same).
func (h *Header) EncodeSize() (int64, error) {
	var acc sizeAcc
	acc.add(4) // magic
	acc.add(stSize(h.Version))
	if err := acc.err; err != nil {
		return 0, err
	}
	if err := h.forEachListSize(&acc); err != nil {
		return 0, err
	}
	return acc.n, acc.err
}

// sizeAcc accumulates byte counts with overflow checks.
type sizeAcc struct {
	n   int64
	err error
}

func (a *sizeAcc) add(x int64) {
	if a.err != nil {
		return
	}
	if x < 0 {
		a.err = errors.New("negative size")
		return
	}
	n, err := addCheck(a.n, x)
	a.n, a.err = n, err
}

// forEachListSize adds the encoded lengths of the three top-level lists.
func (h *Header) forEachListSize(acc *sizeAcc) error {
	// Dimensions.
	acc.add(4)
	acc.add(stSize(h.Version))
	for _, d := range h.Dims {
		acc.add(encNameSize(h.Version, d.Name))
		acc.add(stSize(h.Version))
	}
	// Global attributes.
	acc.add(4)
	acc.add(stSize(h.Version))
	for _, a := range h.Attrs {
		acc.add(encAttrSize(h.Version, a))
	}
	// Variables.
	acc.add(4)
	acc.add(stSize(h.Version))
	for i := range h.Vars {
		acc.add(encVarSize(h.Version, &h.Vars[i]))
	}
	return acc.err
}

func encNameSize(v Version, name string) int64 {
	n := int64(len(name))
	return stSize(v) + n + pad4(n)
}

func encAttrSize(v Version, a Attr) int64 {
	n := int64(len(a.Data))
	return encNameSize(v, a.Name) + 4 + stSize(v) + n + pad4(n)
}

func encVarSize(v Version, vr *Var) int64 {
	n := encNameSize(v, vr.Name) + stSize(v)
	for range vr.DimIDs {
		n += stSize(v)
	}
	n += 4 + stSize(v) // the variable's attribute list header
	for _, a := range vr.Attrs {
		n += encAttrSize(v, a)
	}
	return n + 4 + stSize(v) + ofsSize(v)
}

// WriteHeader writes the classic header in the format's grammar, exactly as
// ReadHeader parses it. The variables' Begin values and the header's NumRecs
// are written as they are; Layout assigns the begins of a new file.
func (h *Header) WriteHeader(w io.Writer) error {
	if w == nil {
		return errors.New("cdf: nil writer")
	}
	if h.Version != Version1 && h.Version != Version2 && h.Version != Version5 {
		return fmt.Errorf("cdf: cannot encode version %d", uint8(h.Version))
	}
	if h.NumRecs < 0 {
		return fmt.Errorf("cdf: negative record count %d", h.NumRecs)
	}
	if limit := recordLimit(h.Version); h.NumRecs > limit {
		return fmt.Errorf("cdf: %d records exceed the %s limit of %d", h.NumRecs, h.Version, limit)
	}
	e := &encWriter{w: w}
	e.bytes([]byte{'C', 'D', 'F', byte(h.Version)})
	e.st(h.Version, h.NumRecs)

	// Dimensions.
	e.listHeader(h.Version, len(h.Dims), tagNCdimension)
	for _, d := range h.Dims {
		e.name(h.Version, d.Name)
		e.st(h.Version, d.Size)
	}
	// Global attributes.
	e.attrs(h.Version, h.Attrs)
	// Variables.
	e.listHeader(h.Version, len(h.Vars), tagNCvariable)
	for i := range h.Vars {
		v := &h.Vars[i]
		e.name(h.Version, v.Name)
		e.st(h.Version, int64(len(v.DimIDs)))
		for _, id := range v.DimIDs {
			e.st(h.Version, id)
		}
		e.attrs(h.Version, v.Attrs)
		// The type tag is a fixed 32-bit integer in every version; only
		// size_t-shaped fields widen in CDF-5.
		e.u32(uint32(v.Type))
		vsize, err := roundUp4(v.DataSize)
		if err != nil {
			return err
		}
		// The stored vsize is always the padded slab, including the
		// single-record-variable case whose records are packed unpadded. A
		// size above 2^32-4 cannot be stored in the 32-bit field; the
		// specification's sentinel 2^32-1 marks such a variable, exactly as
		// netCDF-C writes it (v1hpg.c:993-1004). Validate admits the case only
		// for the last variable of its section (NC_check_vlens).
		if h.Version != Version5 && vsize > math.MaxUint32-3 {
			vsize = math.MaxUint32
		}
		e.st(h.Version, vsize)
		e.ofs(h.Version, v.Begin)
	}
	return e.err
}

// encWriter accumulates the first write error and ignores the rest.
type encWriter struct {
	w   io.Writer
	err error
}

// listHeader writes a list tag and count. An empty list is the specification's
// ABSENT: tag 0, count 0 — never the list's own tag with a zero count.
func (e *encWriter) listHeader(v Version, count int, tag uint32) {
	if count == 0 {
		e.u32(0)
		e.st(v, 0)
		return
	}
	e.u32(tag)
	e.st(v, int64(count))
}

func (e *encWriter) attrs(v Version, as []Attr) {
	e.listHeader(v, len(as), tagNCattribute)
	for _, a := range as {
		e.name(v, a.Name)
		e.u32(uint32(a.Type))
		e.st(v, int64(a.Len()))
		e.bytes(a.Data)
		e.zeros(pad4(int64(len(a.Data))))
	}
}

func (e *encWriter) bytes(p []byte) {
	if e.err != nil {
		return
	}
	_, e.err = e.w.Write(p)
}

//nolint:gosec // big-endian byte extraction: each shift result is a byte by construction
func (e *encWriter) u32(x uint32) {
	e.bytes([]byte{byte(x >> 24), byte(x >> 16), byte(x >> 8), byte(x)})
}

//nolint:gosec // big-endian byte extraction: each shift result is a byte by construction
func (e *encWriter) u64(x uint64) {
	e.bytes([]byte{
		byte(x >> 56), byte(x >> 48), byte(x >> 40), byte(x >> 32),
		byte(x >> 24), byte(x >> 16), byte(x >> 8), byte(x),
	})
}

// st writes a size_t-shaped value: 32 or 64 bits, per the version.
func (e *encWriter) st(v Version, x int64) {
	if x < 0 {
		e.fail(fmt.Errorf("cdf: cannot encode negative size %d", x))
		return
	}
	if v == Version5 {
		e.u64(uint64(x))
		return
	}
	if x > math.MaxUint32 {
		e.fail(fmt.Errorf("cdf: size %d exceeds a 32-bit size_t", x))
		return
	}
	e.u32(uint32(x))
}

// ofs writes a variable's beginning: 32 bits in CDF-1, 64 otherwise.
func (e *encWriter) ofs(v Version, x int64) {
	if x < 0 {
		e.fail(fmt.Errorf("cdf: cannot encode negative offset %d", x))
		return
	}
	if v == Version1 {
		if x > math.MaxInt32 {
			e.fail(fmt.Errorf("cdf: offset %d exceeds the CDF-1 range", x))
			return
		}
		e.u32(uint32(x))
		return
	}
	e.u64(uint64(x))
}

func (e *encWriter) name(v Version, s string) {
	e.st(v, int64(len(s)))
	e.bytes([]byte(s))
	e.zeros(pad4(int64(len(s))))
}

// zeros writes n NUL bytes, in bounded chunks.
func (e *encWriter) zeros(n int64) {
	if n <= 0 || e.err != nil {
		return
	}
	var z [64]byte
	for n > 0 {
		k := n
		if k > int64(len(z)) {
			k = int64(len(z))
		}
		e.bytes(z[:k])
		n -= k
	}
}

func (e *encWriter) fail(err error) {
	if e.err == nil {
		e.err = err
	}
}

// Layout computes every variable's geometry and starting offset exactly as
// netCDF-C's NC_begins with its default alignment (1, no headroom): the fixed
// variables follow the header in definition order, each padded to four bytes,
// then the record variables. The header's NumRecs is left untouched.
func (h *Header) Layout() error {
	if !validVersion(h.Version) {
		return fmt.Errorf("cdf: cannot lay out version %d", uint8(h.Version))
	}
	if err := h.computeShapes(); err != nil {
		return err
	}
	if err := h.computeRecordSize(); err != nil {
		return err
	}
	size, err := h.EncodeSize()
	if err != nil {
		return err
	}
	h.HeaderSize = size

	index, err := h.layoutFixed()
	if err != nil {
		return err
	}
	if err := h.layoutRecords(index); err != nil {
		return err
	}
	if err := h.checkOffsets(); err != nil {
		return err
	}
	return h.checkVarSizes()
}

// layoutFixed assigns the fixed-size variables' begins, in definition order,
// each taking its padded data size; it returns the record section's begin.
func (h *Header) layoutFixed() (int64, error) {
	index := h.HeaderSize
	for i := range h.Vars {
		v := &h.Vars[i]
		if v.Record {
			continue
		}
		if err := h.checkBegin(index); err != nil {
			return 0, err
		}
		v.Begin = index
		padded, err := roundUp4(v.DataSize)
		if err != nil {
			return 0, corruptf("variable %q: %s", v.Name, err.Error())
		}
		if index, err = addCheck(index, padded); err != nil {
			return 0, corruptf("variable %q: %s", v.Name, err.Error())
		}
	}
	return index, nil
}

// layoutRecords assigns the record variables' begins in definition order; the
// single-record-variable case is already reflected in SlabSize.
func (h *Header) layoutRecords(index int64) error {
	for i := range h.Vars {
		v := &h.Vars[i]
		if !v.Record {
			continue
		}
		if err := h.checkBegin(index); err != nil {
			return err
		}
		v.Begin = index
		var err error
		if index, err = addCheck(index, v.SlabSize); err != nil {
			return corruptf("record variable %q: %s", v.Name, err.Error())
		}
	}
	return nil
}

// validVersion reports whether the version is one of the classic formats.
func validVersion(v Version) bool {
	switch v {
	case Version1, Version2, Version5:
		return true
	}
	return false
}

// checkBegin rejects an offset the format cannot store: CDF-1 offsets are
// signed 32-bit values (X_OFF_MAX), the 64-bit forms must stay addressable.
func (h *Header) checkBegin(index int64) error {
	if h.Version == Version1 && index > math.MaxInt32 {
		return fmt.Errorf("cdf: offset %d exceeds the CDF-1 range", index)
	}
	return nil
}

// checkVarSizes mirrors NC_check_vlens: a fixed variable's data and one
// record's slab must be addressable by the format, with a single too-large
// variable tolerated only as the last of its section and, for the fixed
// section, only when no record variables follow. CDF-5 has no exception.
func (h *Header) checkVarSizes() error {
	if len(h.Vars) == 0 {
		return nil
	}
	vlenMax := int64(math.MaxInt64 - 3)
	switch h.Version {
	case Version1:
		vlenMax = math.MaxInt32 - 3
	case Version2:
		vlenMax = math.MaxUint32 - 3
	case Version5:
		// X_INT64_MAX - 3 already, and CDF-5 has no too-large exception.
	}
	tooLarge := func(v *Var) bool { return v.DataSize > vlenMax }
	if err := h.checkSectionSizes(false, tooLarge); err != nil {
		return err
	}
	return h.checkSectionSizes(true, tooLarge)
}

// checkSectionSizes applies the one-exception rule to the fixed or the record
// section. A too-large last fixed variable also forbids a record section,
// exactly as NC_check_vlens does; CDF-5 allows no exception at all.
func (h *Header) checkSectionSizes(records bool, tooLarge func(*Var) bool) error {
	large, last := 0, false
	for i := range h.Vars {
		v := &h.Vars[i]
		if v.Record != records {
			continue
		}
		last = false
		if !tooLarge(v) {
			continue
		}
		if h.Version == Version5 {
			return corruptf("variable %q is too large for CDF-5", v.Name)
		}
		large++
		last = true
	}
	if large > 1 || (large == 1 && !last) {
		return corruptf("more than one too-large variable, or a too-large one that is not last")
	}
	if records || large == 0 {
		return nil
	}
	for i := range h.Vars {
		if h.Vars[i].Record {
			return corruptf("a too-large fixed variable with record variables present")
		}
	}
	return nil
}

// FillBytes returns the default fill value's external representation for t:
// the values NC_FILL_* names, big-endian, one value wide.
func FillBytes(t Type) ([]byte, error) {
	switch t {
	case TypeByte:
		return []byte{0x81}, nil // -127
	case TypeChar:
		return []byte{0x00}, nil
	case TypeShort:
		return []byte{0x80, 0x01}, nil // -32767
	case TypeInt:
		return []byte{0x80, 0x00, 0x00, 0x01}, nil // -2147483647
	case TypeFloat:
		return []byte{0x7c, 0xf0, 0x00, 0x00}, nil // 9.9692099683868690e+36
	case TypeDouble:
		return []byte{0x47, 0x9e, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, nil
	case TypeUByte:
		return []byte{0xff}, nil
	case TypeUShort:
		return []byte{0xff, 0xff}, nil
	case TypeUInt:
		return []byte{0xff, 0xff, 0xff, 0xff}, nil
	case TypeInt64:
		return []byte{0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02}, nil
	case TypeUInt64:
		return []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xfe}, nil
	}
	return nil, fmt.Errorf("cdf: no fill value for type tag %d", uint8(t))
}

// fillChunk bounds the pattern buffer a fill writes at a time.
const fillChunk = 64 << 10

// FillFixed fills every fixed-size variable's padded data region with the
// bytes fill returns for it, as netCDF-C's fillerup does on creation.
func (h *Header) FillFixed(w io.WriterAt, fill func(*Var) []byte) error {
	if w == nil || fill == nil {
		return errors.New("cdf: nil writer or fill function")
	}
	for i := range h.Vars {
		v := &h.Vars[i]
		if v.Record {
			continue
		}
		n, err := roundUp4(v.DataSize)
		if err != nil {
			return corruptf("variable %q: %s", v.Name, err.Error())
		}
		if err := writeFill(w, v.Begin, n, repeatFill(fill(v))); err != nil {
			return fmt.Errorf("cdf: variable %q: %w", v.Name, err)
		}
	}
	return nil
}

// FillRecords fills the record slabs of records [from, to): the padded slab
// for each record variable, or the unpadded slab in the single-variable case
// (NCfillrecord and NCfillspecialrecord).
func (h *Header) FillRecords(w io.WriterAt, from, to int64, fill func(*Var) []byte) error {
	if w == nil || fill == nil {
		return errors.New("cdf: nil writer or fill function")
	}
	if from < 0 || to < from {
		return fmt.Errorf("cdf: invalid record range [%d, %d)", from, to)
	}
	// One pattern per record variable, built once: extending by many records
	// then allocates nothing per record or slab.
	patterns := make([][]byte, len(h.Vars))
	for i := range h.Vars {
		if h.Vars[i].Record {
			patterns[i] = repeatFill(fill(&h.Vars[i]))
		}
	}
	for rec := from; rec < to; rec++ {
		offset, err := mulCheck(rec, h.RecSize)
		if err != nil {
			return corruptf("record %d: %s", rec, err.Error())
		}
		if err := h.fillRecord(w, offset, patterns); err != nil {
			return err
		}
	}
	return nil
}

// fillRecord fills one record's slabs of every record variable.
func (h *Header) fillRecord(w io.WriterAt, offset int64, patterns [][]byte) error {
	for i := range h.Vars {
		v := &h.Vars[i]
		if !v.Record {
			continue
		}
		off, err := addCheck(v.Begin, offset)
		if err != nil {
			return corruptf("record variable %q: %s", v.Name, err.Error())
		}
		if err := writeFill(w, off, v.SlabSize, patterns[i]); err != nil {
			return fmt.Errorf("cdf: record variable %q: %w", v.Name, err)
		}
	}
	return nil
}

// ExtendRecs fills and extends the record section to to records, as a write
// beyond the current count does in netCDF-C. It reports the format's record
// count limit and any offset overflow before writing.
func (h *Header) ExtendRecs(w io.WriterAt, to int64, fill func(*Var) []byte) error {
	if to <= h.NumRecs {
		return nil
	}
	// A count the format cannot address is a selection error, not an I/O one;
	// the kind lets the public package map it while real write errors pass
	// through unchanged.
	if limit := recordLimit(h.Version); to > limit {
		return rangeFf("%d records exceed the %s limit of %d", to, h.Version, limit)
	}
	if h.RecSize > 0 {
		span, err := mulCheck(to, h.RecSize)
		if err != nil {
			return rangeFf("record count %d: %s", to, err.Error())
		}
		if _, err := addCheck(h.recordSectionBegin(), span); err != nil {
			return rangeFf("record count %d: %s", to, err.Error())
		}
	}
	if err := h.FillRecords(w, h.NumRecs, to, fill); err != nil {
		return err
	}
	h.NumRecs = to
	return nil
}

// repeatFill returns a fillChunk-sized buffer holding val repeated. Every fill
// value width (1, 2, 4 or 8 bytes) divides the chunk size.
func repeatFill(val []byte) []byte {
	if len(val) == 0 {
		return nil
	}
	pattern := make([]byte, fillChunk)
	for i := 0; i < len(pattern); i += len(val) {
		copy(pattern[i:], val)
	}
	return pattern
}

// writeFill writes n bytes of the repeated fill pattern at off.
func writeFill(w io.WriterAt, off, n int64, pattern []byte) error {
	if n <= 0 {
		return nil
	}
	if len(pattern) == 0 {
		return errors.New("empty fill pattern")
	}
	for done := int64(0); done < n; {
		k := n - done
		if k > int64(len(pattern)) {
			k = int64(len(pattern))
		}
		if _, err := w.WriteAt(pattern[:k], off+done); err != nil {
			return err
		}
		done += k
	}
	return nil
}

// recordLimit returns the largest record count the version's numrecs field can
// hold. CDF-1 and CDF-2 store a 32-bit NON_NEG whose only reserved value is
// 0xFFFFFFFF (the STREAMING marker), so the maximum is 2^32-2; CDF-5 stores 64
// bits, and the package's counts are int64.
func recordLimit(v Version) int64 {
	if v == Version5 {
		return math.MaxInt64
	}
	return math.MaxUint32 - 1
}

// WriteNumRecs writes the header's record count at its fixed position, after
// the four-byte magic. Close calls it once the data is written.
func (h *Header) WriteNumRecs(w io.WriterAt) error {
	if w == nil {
		return errors.New("cdf: nil writer")
	}
	if h.NumRecs < 0 {
		return fmt.Errorf("cdf: negative record count %d", h.NumRecs)
	}
	var b [8]byte
	var n int64
	if h.Version == Version5 {
		binary.BigEndian.PutUint64(b[:], uint64(h.NumRecs))
		n = 8
	} else {
		if limit := recordLimit(h.Version); h.NumRecs > limit {
			return fmt.Errorf("cdf: %d records exceed the %s limit of %d", h.NumRecs, h.Version, limit)
		}
		binary.BigEndian.PutUint32(b[:], uint32(h.NumRecs)) //nolint:gosec // the limit above bounds the value to 32 bits
		n = 4
	}
	_, err := w.WriteAt(b[:n], 4)
	return err
}

// Validate checks a header built for writing against every rule the format
// imposes: names, types, list sizes, the unlimited dimension and _FillValue.
// ReadHeader applies the same rules to input; a header that passes Validate
// and Layout is one the reader accepts back.
func (h *Header) Validate() error {
	if !validVersion(h.Version) {
		return fmt.Errorf("cdf: version %d is not a classic format", uint8(h.Version))
	}
	if h.NumRecs != 0 {
		return fmt.Errorf("cdf: a new file starts with 0 records, not %d", h.NumRecs)
	}
	if err := h.validateDims(); err != nil {
		return err
	}
	if err := checkAttrList(h.Version, h.Attrs, "global attribute"); err != nil {
		return err
	}
	return h.validateVars()
}

// validateDims checks the dimension list, including the single-unlimited rule.
func (h *Header) validateDims() error {
	if len(h.Dims) > maxEntries {
		return fmt.Errorf("cdf: %d dimensions exceed the %d-entry limit", len(h.Dims), int64(maxEntries))
	}
	seen := make(map[string]bool, len(h.Dims))
	unlimited := 0
	for _, d := range h.Dims {
		if err := CheckName(d.Name); err != nil {
			return err
		}
		if seen[d.Name] {
			return fmt.Errorf("cdf: duplicate dimension %q", d.Name)
		}
		seen[d.Name] = true
		if d.Size < 0 {
			return fmt.Errorf("cdf: dimension %q has length %d", d.Name, d.Size)
		}
		if h.Version != Version5 && d.Size > math.MaxUint32 {
			return fmt.Errorf("cdf: dimension %q has length %d, above the %s size_t",
				d.Name, d.Size, h.Version)
		}
		if d.Size == 0 {
			unlimited++
		}
	}
	if unlimited > 1 {
		return fmt.Errorf("cdf: %d dimensions have length zero, at most one unlimited dimension is allowed", unlimited)
	}
	return nil
}

// validateVars checks the variable list.
func (h *Header) validateVars() error {
	if len(h.Vars) > maxEntries {
		return fmt.Errorf("cdf: %d variables exceed the %d-entry limit", len(h.Vars), int64(maxEntries))
	}
	seen := make(map[string]bool, len(h.Vars))
	for i := range h.Vars {
		if err := h.validateVar(&h.Vars[i], seen); err != nil {
			return err
		}
	}
	return nil
}

// validateVar checks one variable: its name, dimensions, type, attributes and
// _FillValue.
func (h *Header) validateVar(v *Var, seen map[string]bool) error {
	if err := CheckName(v.Name); err != nil {
		return err
	}
	if seen[v.Name] {
		return fmt.Errorf("cdf: duplicate variable %q", v.Name)
	}
	seen[v.Name] = true
	if len(v.DimIDs) > maxVarDims {
		return fmt.Errorf("cdf: variable %q has rank %d, the limit is %d", v.Name, len(v.DimIDs), maxVarDims)
	}
	for j, id := range v.DimIDs {
		if id < 0 || id >= int64(len(h.Dims)) {
			return fmt.Errorf("cdf: variable %q references dimension %d of %d", v.Name, id, len(h.Dims))
		}
		if h.Dims[id].Size == 0 && j != 0 {
			return fmt.Errorf("cdf: variable %q uses the unlimited dimension at position %d", v.Name, j)
		}
	}
	if !v.Type.validFor(h.Version) {
		return fmt.Errorf("cdf: variable %q has type %d, not valid in %s", v.Name, uint8(v.Type), h.Version)
	}
	if err := checkAttrList(h.Version, v.Attrs, fmt.Sprintf("variable %q attribute", v.Name)); err != nil {
		return err
	}
	return checkFillValue(v)
}

// checkAttrList validates one attribute list.
func checkAttrList(version Version, attrs []Attr, what string) error {
	if len(attrs) > maxEntries {
		return fmt.Errorf("cdf: %d %ss exceed the %d-entry limit", len(attrs), what, int64(maxEntries))
	}
	seen := make(map[string]bool, len(attrs))
	for _, a := range attrs {
		if err := CheckName(a.Name); err != nil {
			return err
		}
		if seen[a.Name] {
			return fmt.Errorf("cdf: duplicate %q %q", what, a.Name)
		}
		seen[a.Name] = true
		if !a.Type.validFor(version) {
			return fmt.Errorf("cdf: %s %q has type %d, not valid in %s", what, a.Name, uint8(a.Type), version)
		}
		size := int64(a.Type.Size())
		if size <= 0 || int64(len(a.Data))%size != 0 {
			return fmt.Errorf("cdf: %s %q holds %d bytes, not a whole number of %s values",
				what, a.Name, len(a.Data), typeName(a.Type))
		}
		if int64(len(a.Data)) > maxAttrBytes {
			return fmt.Errorf("cdf: %s %q holds %d bytes, above the %d-byte limit",
				what, a.Name, len(a.Data), int64(maxAttrBytes))
		}
		if a.Len() > math.MaxInt32 {
			return fmt.Errorf("cdf: %s %q holds %d values, the limit is %d",
				what, a.Name, a.Len(), int64(math.MaxInt32))
		}
	}
	return nil
}

// checkFillValue enforces what NC3_def_var_fill guarantees: a _FillValue has
// the variable's own type and exactly one value.
func checkFillValue(v *Var) error {
	for _, a := range v.Attrs {
		if a.Name != "_FillValue" {
			continue
		}
		if a.Type != v.Type {
			return fmt.Errorf("cdf: variable %q: _FillValue has type %s, the variable has %s",
				v.Name, typeName(a.Type), typeName(v.Type))
		}
		if a.Len() != 1 {
			return fmt.Errorf("cdf: variable %q: _FillValue holds %d values, want exactly 1",
				v.Name, a.Len())
		}
	}
	return nil
}

// typeName names a type for error messages without the public package's String.
func typeName(t Type) string {
	switch t {
	case TypeByte:
		return "byte"
	case TypeChar:
		return "char"
	case TypeShort:
		return "short"
	case TypeInt:
		return "int"
	case TypeFloat:
		return "float"
	case TypeDouble:
		return "double"
	case TypeUByte:
		return "ubyte"
	case TypeUShort:
		return "ushort"
	case TypeUInt:
		return "uint"
	case TypeInt64:
		return "int64"
	case TypeUInt64:
		return "uint64"
	}
	return fmt.Sprintf("Type(%d)", uint8(t))
}

// CheckName validates a name by netCDF-C's NC_check_name rules: non-empty,
// valid UTF-8, no '/', the first character is [A-Za-z0-9_] or a multibyte
// UTF-8 character, later characters are printable ASCII or UTF-8, no trailing
// space, at most NC_MAX_NAME (256) bytes.
func CheckName(name string) error {
	if name == "" {
		return errors.New("cdf: empty name")
	}
	if len(name) > 256 {
		return fmt.Errorf("cdf: name is %d bytes, the limit is 256", len(name))
	}
	if !utf8.ValidString(name) {
		return fmt.Errorf("cdf: name %q is not valid UTF-8", name)
	}
	r, size := utf8.DecodeRuneInString(name)
	if r < utf8.RuneSelf {
		if !isNameStart(r) {
			return fmt.Errorf("cdf: name %q starts with %q", name, r)
		}
	}
	for _, r := range name[size:] {
		// Non-ASCII runes are valid UTF-8 here already (utf8.ValidString).
		if r < utf8.RuneSelf && (r < ' ' || r > '~') {
			return fmt.Errorf("cdf: name %q holds the control character %q", name, r)
		}
	}
	if name[len(name)-1] == ' ' {
		return fmt.Errorf("cdf: name %q ends with a space", name)
	}
	if strings.IndexByte(name, '/') >= 0 {
		return fmt.Errorf("cdf: name %q holds '/'", name)
	}
	return nil
}

func isNameStart(r rune) bool {
	return r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_'
}
