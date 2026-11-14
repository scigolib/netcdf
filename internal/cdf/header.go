package cdf

import (
	"errors"
	"fmt"
	"io"
	"math"
)

// List tags from the specification's grammar.
const (
	tagNCdimension = 10
	tagNCvariable  = 11
	tagNCattribute = 12
)

// Limits that keep a crafted header from turning into an oversized allocation.
// They are far beyond any real file: netCDF-C's long-standing defaults were
// 1024 dimensions, 8192 variables and 8192 attributes per list.
const (
	maxEntries   = 1_000_000 // entries in one list
	maxVarDims   = 1024      // NC_MAX_VAR_DIMS, enforced by netCDF-C too
	maxNameLen   = 64 << 10  // bytes in one name
	maxAttrBytes = 128 << 20 // bytes of values in one attribute
	maxAlloc     = 256 << 20 // largest single allocation the parser makes
)

// ReadHeader parses the header of the classic file r whose total length is
// size, and returns it with every variable's geometry computed and validated.
//
// Input that does not start with a classic magic yields ErrNotNetCDF; input
// that starts like one but violates the format yields an error wrapping
// ErrCorrupt.
func ReadHeader(r io.ReaderAt, size int64) (*Header, error) {
	if r == nil {
		return nil, errors.New("cdf: nil reader")
	}
	if size < 4 {
		return nil, notNetCDFf("%d bytes", size)
	}
	c := newCursor(r, size)
	version, err := readMagic(c)
	if err != nil {
		return nil, err
	}
	h := &Header{Version: version}

	nrecs, err := c.st(version)
	if err != nil {
		return nil, err
	}
	// The 32-bit STREAMING marker means the record count is not stored; the
	// specification names the file length as the way to recover it. CDF-5 has
	// a 64-bit count and no marker (netCDF-C does not implement streaming).
	streaming := version != Version5 && nrecs == 0xFFFFFFFF
	h.NumRecs = nrecs

	if err := h.parseDims(c); err != nil {
		return nil, err
	}
	if h.Attrs, err = h.parseAttrs(c); err != nil {
		return nil, err
	}
	if err := h.parseVars(c); err != nil {
		return nil, err
	}
	h.HeaderSize = c.pos

	if err := h.layout(size, streaming); err != nil {
		return nil, err
	}
	return h, nil
}

// readMagic consumes the four-byte magic and returns the format version.
func readMagic(c *cursor) (Version, error) {
	magic, err := c.window(4)
	if err != nil {
		return 0, err
	}
	if magic[0] != 'C' || magic[1] != 'D' || magic[2] != 'F' {
		return 0, notNetCDFf("magic %q is not CDF", string(magic[:3]))
	}
	c.pos = 4
	switch magic[3] {
	case 1:
		return Version1, nil
	case 2:
		return Version2, nil
	case 5:
		return Version5, nil
	}
	return 0, notNetCDFf("version byte %d", magic[3])
}

func (h *Header) parseDims(c *cursor) error {
	tag, err := c.u32()
	if err != nil {
		return err
	}
	count, err := c.st(h.Version)
	if err != nil {
		return err
	}
	if err := h.checkListHeader(c, tag, count, tagNCdimension, "dimension", minDimEntry(h.Version)); err != nil {
		return err
	}
	if count == 0 {
		return nil
	}
	h.Dims = make([]Dim, 0, count)
	unlimited := 0
	for i := int64(0); i < count; i++ {
		d, err := h.parseDim(c)
		if err != nil {
			return err
		}
		if d.Size == 0 {
			unlimited++
		}
		h.Dims = append(h.Dims, d)
	}
	// "There can be at most one record dimension": a second zero-length
	// dimension would make two unrelated variables record variables.
	if unlimited > 1 {
		return c.corrupt("%d dimensions have length zero, at most one unlimited dimension is allowed", unlimited)
	}
	return nil
}

func (h *Header) parseDim(c *cursor) (Dim, error) {
	name, err := c.name(h.Version)
	if err != nil {
		return Dim{}, err
	}
	size, err := c.st(h.Version)
	if err != nil {
		return Dim{}, err
	}
	if size < 0 {
		return Dim{}, c.corrupt("dimension %q has length %d", name, size)
	}
	return Dim{Name: name, Size: size}, nil
}

// parseAttrs reads one attribute list, at the global level or of a variable.
func (h *Header) parseAttrs(c *cursor) ([]Attr, error) {
	tag, err := c.u32()
	if err != nil {
		return nil, err
	}
	count, err := c.st(h.Version)
	if err != nil {
		return nil, err
	}
	if err := h.checkListHeader(c, tag, count, tagNCattribute, "attribute", minAttrEntry(h.Version)); err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}
	attrs := make([]Attr, 0, count)
	for i := int64(0); i < count; i++ {
		a, err := h.parseAttr(c)
		if err != nil {
			return nil, err
		}
		attrs = append(attrs, a)
	}
	return attrs, nil
}

func (h *Header) parseAttr(c *cursor) (Attr, error) {
	name, err := c.name(h.Version)
	if err != nil {
		return Attr{}, err
	}
	typ, err := h.readType(c, fmt.Sprintf("attribute %q", name))
	if err != nil {
		return Attr{}, err
	}
	nelems, err := c.st(h.Version)
	if err != nil {
		return Attr{}, err
	}
	if nelems < 0 {
		return Attr{}, c.corrupt("attribute %q has %d values", name, nelems)
	}
	pix := int64(typ.Size())
	if nelems > maxAttrBytes/pix {
		return Attr{}, c.corrupt("attribute %q declares %d values, above the %d-byte limit",
			name, nelems, int64(maxAttrBytes))
	}
	total := nelems * pix
	data, err := c.bytes(total)
	if err != nil {
		return Attr{}, err
	}
	if err := c.skip(pad4(total)); err != nil {
		return Attr{}, err
	}
	return Attr{Name: name, Type: typ, Data: data}, nil
}

func (h *Header) parseVars(c *cursor) error {
	tag, err := c.u32()
	if err != nil {
		return err
	}
	count, err := c.st(h.Version)
	if err != nil {
		return err
	}
	if err := h.checkListHeader(c, tag, count, tagNCvariable, "variable", minVarEntry(h.Version)); err != nil {
		return err
	}
	if count == 0 {
		return nil
	}
	h.Vars = make([]Var, 0, count)
	for i := int64(0); i < count; i++ {
		v, err := h.parseVar(c)
		if err != nil {
			return err
		}
		h.Vars = append(h.Vars, v)
	}
	return nil
}

func (h *Header) parseVar(c *cursor) (Var, error) {
	name, err := c.name(h.Version)
	if err != nil {
		return Var{}, err
	}
	rank, err := c.st(h.Version)
	if err != nil {
		return Var{}, err
	}
	if rank < 0 || rank > maxVarDims {
		return Var{}, c.corrupt("variable %q has rank %d, the limit is %d", name, rank, int64(maxVarDims))
	}
	ids := make([]int64, 0, rank)
	for j := int64(0); j < rank; j++ {
		id, err := c.st(h.Version)
		if err != nil {
			return Var{}, err
		}
		if id < 0 {
			return Var{}, c.corrupt("variable %q has negative dimension id %d", name, id)
		}
		ids = append(ids, id)
	}
	attrs, err := h.parseAttrs(c)
	if err != nil {
		return Var{}, err
	}
	typ, err := h.readType(c, fmt.Sprintf("variable %q", name))
	if err != nil {
		return Var{}, err
	}
	// The vsize field is redundant by the specification's own note: it is
	// recomputed from the dimensions (netCDF-C does the same, var.c:520).
	// It is read only to advance, never trusted.
	if _, err := c.st(h.Version); err != nil {
		return Var{}, err
	}
	begin, err := c.ofs(h.Version)
	if err != nil {
		return Var{}, err
	}
	return Var{Name: name, DimIDs: ids, Attrs: attrs, Type: typ, Begin: begin}, nil
}

// readType reads and validates a 32-bit type tag against the format version.
func (h *Header) readType(c *cursor, what string) (Type, error) {
	code, err := c.u32()
	if err != nil {
		return 0, err
	}
	typ, ok := typeFromTag(code)
	if !ok || !typ.validFor(h.Version) {
		return 0, c.corrupt("%s has type tag %d, not valid in %s", what, code, h.Version)
	}
	return typ, nil
}

// checkListHeader validates a list's tag and count. A zero count is an absent
// list, which the specification writes as tag 0 and count 0; netCDF-C also
// accepts the list's own tag with a zero count, so both are allowed.
func (h *Header) checkListHeader(c *cursor, tag uint32, count int64, want uint32, what string, minEntry int64) error {
	if count < 0 {
		return c.corrupt("negative %s count %d", what, count)
	}
	if count == 0 {
		if tag != 0 && tag != want {
			return c.corrupt("empty %s list with tag %d", what, tag)
		}
		return nil
	}
	if tag != want {
		return c.corrupt("%s list tag %d, want %d", what, tag, want)
	}
	if count > maxEntries {
		return c.corrupt("%d %s entries exceed the %d-entry limit", count, what, int64(maxEntries))
	}
	if count > c.remaining()/minEntry {
		return c.corrupt("%d %s entries cannot fit in the %d remaining bytes", count, what, c.remaining())
	}
	return nil
}

// layout computes each variable's shape, strides and slab size, resolves the
// record count, and validates the starting offsets against the section order.
func (h *Header) layout(size int64, streaming bool) error {
	if err := h.computeShapes(); err != nil {
		return err
	}
	if err := h.computeRecordSize(); err != nil {
		return err
	}
	if err := h.checkOffsets(); err != nil {
		return err
	}
	if err := h.resolveNumRecs(size, streaming); err != nil {
		return err
	}
	return h.checkSpans()
}

func (h *Header) computeShapes() error {
	for i := range h.Vars {
		if err := h.computeShape(&h.Vars[i]); err != nil {
			return err
		}
	}
	return nil
}

func (h *Header) computeShape(v *Var) error {
	v.ElemSize = v.Type.Size()
	v.Shape = make([]int64, len(v.DimIDs))
	product := int64(1)
	for j, id := range v.DimIDs {
		if id < 0 || id >= int64(len(h.Dims)) {
			return corruptf("variable %q references dimension %d of %d", v.Name, id, len(h.Dims))
		}
		dimSize := h.Dims[id].Size
		if dimSize == 0 {
			if j != 0 {
				return corruptf("variable %q uses the unlimited dimension at position %d", v.Name, j)
			}
			v.Record = true
		}
		v.Shape[j] = dimSize
		if v.Record && j == 0 {
			continue
		}
		var err error
		if product, err = mulCheck(product, dimSize); err != nil {
			return corruptf("variable %q: %s", v.Name, err.Error())
		}
	}
	var err error
	if v.DataSize, err = mulCheck(product, int64(v.ElemSize)); err != nil {
		return corruptf("variable %q: %s", v.Name, err.Error())
	}
	v.Strides = strides(v.Shape)
	return nil
}

// computeRecordSize sums the per-record slabs, each padded to four bytes,
// except the specification's special case of exactly one record variable,
// whose slabs are packed without padding (v1hpg.c:1298 in netCDF-C).
func (h *Header) computeRecordSize() error {
	var recVars []*Var
	for i := range h.Vars {
		if h.Vars[i].Record {
			recVars = append(recVars, &h.Vars[i])
		}
	}
	h.RecSize = 0
	if len(recVars) == 1 {
		recVars[0].SlabSize = recVars[0].DataSize
		h.RecSize = recVars[0].DataSize
		return nil
	}
	for _, v := range recVars {
		slab, err := roundUp4(v.DataSize)
		if err != nil {
			return corruptf("variable %q: %s", v.Name, err.Error())
		}
		v.SlabSize = slab
		if h.RecSize, err = addCheck(h.RecSize, slab); err != nil {
			return corruptf("record size: %s", err.Error())
		}
	}
	return nil
}

// checkOffsets validates the section order: fixed-size variables first, in
// definition order, then the record variables (netCDF-C NC_check_voffs).
func (h *Header) checkOffsets() error {
	fixedEnd, err := h.checkFixedOffsets()
	if err != nil {
		return err
	}
	return h.checkRecordOffsets(fixedEnd)
}

func (h *Header) checkFixedOffsets() (int64, error) {
	prev := h.HeaderSize
	for i := range h.Vars {
		v := &h.Vars[i]
		if v.Record {
			continue
		}
		if v.Begin < prev {
			return 0, corruptf("variable %q begins at %d, before %d", v.Name, v.Begin, prev)
		}
		padded, err := roundUp4(v.DataSize)
		if err != nil {
			return 0, corruptf("variable %q: %s", v.Name, err.Error())
		}
		if prev, err = addCheck(v.Begin, padded); err != nil {
			return 0, corruptf("variable %q: %s", v.Name, err.Error())
		}
	}
	return prev, nil
}

func (h *Header) checkRecordOffsets(fixedEnd int64) error {
	sectionBegin := int64(-1)
	prevEnd := int64(-1)
	for i := range h.Vars {
		v := &h.Vars[i]
		if !v.Record {
			continue
		}
		if sectionBegin < 0 {
			if v.Begin < fixedEnd {
				return corruptf("record section begins at %d, before %d", v.Begin, fixedEnd)
			}
			sectionBegin = v.Begin
			prevEnd = v.Begin
		}
		if v.Begin < prevEnd {
			return corruptf("record variable %q begins at %d, before %d", v.Name, v.Begin, prevEnd)
		}
		var err error
		if prevEnd, err = addCheck(v.Begin, v.SlabSize); err != nil {
			return corruptf("record variable %q: %s", v.Name, err.Error())
		}
	}
	return nil
}

// resolveNumRecs turns the STREAMING marker into a record count from the file
// length, as the specification's note names the method. A file shorter than
// its first record holds zero records.
func (h *Header) resolveNumRecs(size int64, streaming bool) error {
	if !streaming {
		return nil
	}
	h.NumRecs = 0
	if h.RecSize == 0 {
		return nil
	}
	begin := h.recordSectionBegin()
	if size <= begin {
		return nil
	}
	h.NumRecs = (size - begin) / h.RecSize
	return nil
}

func (h *Header) recordSectionBegin() int64 {
	for i := range h.Vars {
		if h.Vars[i].Record {
			return h.Vars[i].Begin
		}
	}
	return h.HeaderSize
}

// checkSpans proves that every variable's last byte is addressable in int64
// arithmetic, so the walk code needs no further overflow checks.
func (h *Header) checkSpans() error {
	recordSpan := int64(0)
	if h.RecSize > 0 {
		span, err := mulCheck(h.NumRecs, h.RecSize)
		if err != nil {
			return corruptf("record count %d: %s", h.NumRecs, err.Error())
		}
		recordSpan = span
	}
	for i := range h.Vars {
		v := &h.Vars[i]
		span := v.DataSize
		if v.Record {
			span = recordSpan
		}
		if _, err := addCheck(v.Begin, span); err != nil {
			return corruptf("variable %q: %s", v.Name, err.Error())
		}
	}
	return nil
}

// strides returns each dimension's element stride within one slab. For a
// record variable the stride of the record dimension is the slab's element
// count; it is not used for offsets (records are addressed through RecSize).
func strides(shape []int64) []int64 {
	s := make([]int64, len(shape))
	acc := int64(1)
	for i := len(shape) - 1; i >= 0; i-- {
		s[i] = acc
		acc *= shape[i]
	}
	return s
}

func mulCheck(a, b int64) (int64, error) {
	if a < 0 || b < 0 {
		return 0, errors.New("negative operand")
	}
	if a != 0 && b > math.MaxInt64/a {
		return 0, errors.New("size overflows")
	}
	return a * b, nil
}

func addCheck(a, b int64) (int64, error) {
	if a < 0 || b < 0 {
		return 0, errors.New("negative operand")
	}
	if a > math.MaxInt64-b {
		return 0, errors.New("offset overflows")
	}
	return a + b, nil
}

func roundUp4(n int64) (int64, error) {
	if n < 0 || n > math.MaxInt64-3 {
		return 0, errors.New("size overflows")
	}
	return (n + 3) &^ 3, nil
}

// Min entry sizes: a name length (size_t), and the fixed fields that follow.

func stSize(v Version) int64 {
	if v == Version5 {
		return 8
	}
	return 4
}

func ofsSize(v Version) int64 {
	if v == Version1 {
		return 4
	}
	return 8
}

func minDimEntry(v Version) int64  { return stSize(v) + stSize(v) }
func minAttrEntry(v Version) int64 { return stSize(v) + 4 + stSize(v) }
func minVarEntry(v Version) int64 {
	return stSize(v) + stSize(v) + (4 + stSize(v)) + 4 + stSize(v) + ofsSize(v)
}
