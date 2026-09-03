// Package fieldwiring isolates the package-local field-wiring rule: a
// pointer- or interface-typed field of an unexported struct that every
// construction in the package's non-test files proves non-nil, and nothing
// invalidates, is treated as always wired and its uses are not reported.
package fieldwiring

type depIface interface {
	Do()
}

type depImpl struct{}

func (*depImpl) Do() {}

// wiredSingle is constructed once, with the field proven non-nil, and never
// invalidated: its field is always wired.
type wiredSingle struct {
	dep depIface
}

func newWiredSingle() *wiredSingle { // want newWiredSingle:"never returns nil result 0"
	return &wiredSingle{dep: &depImpl{}}
}

func (w *wiredSingle) Use() {
	w.dep.Do()
}

// omittedField is constructed once, but the constructor omits the field, so
// it is never wired.
type omittedField struct {
	dep depIface
}

func newOmittedField() *omittedField { // want newOmittedField:"never returns nil result 0"
	return &omittedField{}
}

func (o *omittedField) Use() {
	o.dep.Do() // want "o\\.dep may be nil here"
}

// oneOfTwoOmits has two constructors, one of which omits the field, so it is
// never wired even though the other constructor sets it.
type oneOfTwoOmits struct {
	dep depIface
}

func newOneOfTwoOmitsA() *oneOfTwoOmits { // want newOneOfTwoOmitsA:"never returns nil result 0"
	return &oneOfTwoOmits{dep: &depImpl{}}
}

func newOneOfTwoOmitsB() *oneOfTwoOmits { // want newOneOfTwoOmitsB:"never returns nil result 0"
	return &oneOfTwoOmits{}
}

func (o *oneOfTwoOmits) Use() {
	o.dep.Do() // want "o\\.dep may be nil here"
}

// explicitNil is constructed with the field explicitly set to nil, so it is
// never wired.
type explicitNil struct {
	dep depIface
}

func newExplicitNil() *explicitNil { // want newExplicitNil:"never returns nil result 0"
	return &explicitNil{dep: nil}
}

func (e *explicitNil) Use() {
	e.dep.Do() // want "e\\.dep may be nil here"
}

// zeroValueVar is declared with var somewhere in the package, so its zero
// value is reachable and it is never wired, even though another constructor
// wires it properly.
type zeroValueVar struct {
	dep depIface
}

func newZeroValueVar() *zeroValueVar { // want newZeroValueVar:"never returns nil result 0"
	return &zeroValueVar{dep: &depImpl{}}
}

func makeZeroValueVar() zeroValueVar {
	var z zeroValueVar

	return z
}

func (z *zeroValueVar) Use() {
	z.dep.Do() // want "z\\.dep may be nil here"
}

// sliceElement is reachable as an element of a slice, whose elements start
// zero-valued, so it is never wired even though its own constructor wires it.
type sliceElement struct {
	dep depIface
}

func newSliceElement() *sliceElement { // want newSliceElement:"never returns nil result 0"
	return &sliceElement{dep: &depImpl{}}
}

func makeSliceElements() []sliceElement {
	return make([]sliceElement, 2)
}

func (s *sliceElement) Use() {
	s.dep.Do() // want "s\\.dep may be nil here"
}

// fieldCleared is constructed with the field proven non-nil, but something
// elsewhere in the package clears it, so it is never wired.
type fieldCleared struct {
	dep depIface
}

func newFieldCleared() *fieldCleared { // want newFieldCleared:"never returns nil result 0"
	return &fieldCleared{dep: &depImpl{}}
}

func clearFieldCleared(f *fieldCleared) {
	f.dep = nil
}

func (f *fieldCleared) Use() {
	f.dep.Do() // want "f\\.dep may be nil here"
}

// ExportedWired has the identical shape to wiredSingle, but is exported, so
// the rule must not apply to it: external code can zero-construct it.
type ExportedWired struct {
	dep depIface
}

func newExportedWired() *ExportedWired { // want newExportedWired:"never returns nil result 0"
	return &ExportedWired{dep: &depImpl{}}
}

func (e *ExportedWired) Use() {
	e.dep.Do() // want "e\\.dep may be nil here"
}

// embeddedTarget is wired on its own, but it is embedded by value in the
// exported ExportedHolder below, so external code can zero-construct that
// and reach it with a nil field.
type embeddedTarget struct {
	dep depIface
}

func newEmbeddedTarget() *embeddedTarget { // want newEmbeddedTarget:"never returns nil result 0"
	return &embeddedTarget{dep: &depImpl{}}
}

func (e *embeddedTarget) Use() {
	e.dep.Do() // want "e\\.dep may be nil here"
}

// ExportedHolder embeds embeddedTarget by value, which is what invalidates
// embeddedTarget's own wiring above.
type ExportedHolder struct {
	embeddedTarget
}

// nonPointerField has a field of non-pointer, non-interface type, which the
// wiring rule leaves alone either way: nothing to report and nothing to
// silence.
type nonPointerField struct {
	count int
}

func newNonPointerField() *nonPointerField { // want newNonPointerField:"never returns nil result 0"
	return &nonPointerField{}
}

func (n *nonPointerField) Use() int {
	return n.count
}

// checkedErrorCtor mirrors the billing constructor shape: the dependency
// comes from a call whose error result is checked, with the failure branch
// returning, before being placed in the composite literal. That
// checked-error postcondition is enough to prove the field non-nil.
type checkedErrorCtor struct {
	dep depIface
}

func makeDep() (depIface, error) { // want makeDep:"never returns nil result 0"
	return &depImpl{}, nil
}

func newCheckedErrorCtor() (*checkedErrorCtor, error) {
	dep, err := makeDep()
	if err != nil {
		return nil, err
	}

	return &checkedErrorCtor{dep: dep}, nil
}

func (c *checkedErrorCtor) Use() {
	c.dep.Do()
}

// testOnlyPartial is fully wired by its only production constructor; the
// partial construction in fieldwiring_test.go must not count against it.
type testOnlyPartial struct {
	dep depIface
}

func newTestOnlyPartial() *testOnlyPartial { // want newTestOnlyPartial:"never returns nil result 0"
	return &testOnlyPartial{dep: &depImpl{}}
}

func (t *testOnlyPartial) Use() {
	t.dep.Do()
}
