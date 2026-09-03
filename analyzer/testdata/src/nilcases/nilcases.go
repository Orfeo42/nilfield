// Package nilcases is a corpus of every way a nil value reaches a dereference
// in Go. It is the yardstick this analyzer is measured against, and it is
// deliberately wider than what the analyzer covers today: run a candidate tool
// over the package and compare its output against the markers below.
//
// It is project-agnostic apart from assertpkg/utility, which exists only so the
// imported-assert proof has something to import.
//
// Markers, one per case, on the last line of the function's doc comment:
//
//	//nilhazard:<id> sites=<n>
//	    n places in this function where a nil value reaches a dereference. A
//	    tool reporting fewer than n does not cover the case.
//
//	//nilsafe:<id>
//	    legal and correct. Any diagnostic here is a false positive.
//
//	//niloutofscope:<id> why=<token>
//	    a real hazard that falls outside what this analyzer covers by design.
//	    <token> is one of: not-a-dereference (the hazard is not a nil pointer
//	    or nil interface being dereferenced), not-nil-analysis (the hazard is
//	    not about nil at all), construction-site (the hazard is a struct built
//	    with a field left nil, not a use of that field). Any diagnostic here is
//	    a false positive, the same as a safe case.
//
// TestCorpus turns each marker into a subtest: a hazard case fails until every
// site it holds is reported, a safe or out-of-scope case fails as soon as
// anything is reported in it. The suite is red until the analyzer covers the
// whole corpus, and the failing subtests are the work list.
//
// Sites the analyzer already reports additionally carry an analysistest want
// annotation, which pins the exact position and message. The two safe cases
// below carrying one are false positives: the annotation records what the
// analyzer does today so analysistest stays quiet and the case's own subtest is
// the single place the defect is reported.
//
// Nothing here is meant to run; several cases panic or deadlock by design.
package nilcases

import (
	"errors"

	"assertpkg/utility"
)

type Doer interface {
	Do() int
}

type inner struct {
	n     int
	items []int
	m     map[string]int
	ch    chan int
	fn    func() int
	next  *inner
}

//nilfixture:shared
func (i *inner) touch() {} // want touch:"nil-safe receiver"

//nilfixture:shared
func (i *inner) value() int { return i.n }

//nilfixture:shared
func (i *inner) safeOnNil() bool { return i == nil } // want safeOnNil:"nil-safe receiver"

//nilfixture:shared
func (i *inner) unsafeOnNil() int {
	return i.n
}

//nilfixture:shared
func (i *inner) delegating() bool { return i.safeOnNil() } // want delegating:"nil-safe receiver"

type embedded struct {
	e int
}

type outer struct {
	*embedded

	p     *inner
	q     *inner
	iface Doer
}

//nilfixture:shared
func (o *outer) validate() error { // want validate:"validates p, q"
	if o.p == nil {
		return errNilP
	}

	if o.q == nil {
		return errNilQ
	}

	return nil
}

type service struct {
	dep    Doer
	repo   *inner
	logger *inner
}

type ptrDoer struct {
	n int
}

//nilfixture:shared
func (p *ptrDoer) Do() int { return p.n }

type realDoer struct{}

//nilfixture:shared
func (realDoer) Do() int { return 0 }

var (
	errNilP      = errors.New("p is nil")
	errNilQ      = errors.New("q is nil")
	errConstruct = errors.New("construction failed")
)

//nilfixture:shared
func doWork() error { return nil }

//nilfixture:shared
func abs(n int) int {
	if n < 0 {
		return -n
	}

	return n
}

//nilfixture:shared
func assert(cond bool, msg string) { // want assert:"asserts argument 0"
	if !cond {
		panic(msg)
	}
}

// ---------------------------------------------------------------------------
// A. Construction: a field that is never wired.
// The struct compiles, the zero value is nil, the panic lands on first use.
// ---------------------------------------------------------------------------

// dep is omitted from the literal. Constructing a wiring struct with a field
// left nil is a construction-site defect, not a use of that field.
//
//niloutofscope:new-service-missing-interface-field why=construction-site
func newServiceMissingInterfaceField() *service { // want newServiceMissingInterfaceField:"never returns nil result 0"
	return &service{repo: &inner{}, logger: &inner{}}
}

// repo is omitted from the literal.
//
//niloutofscope:new-service-missing-pointer-field why=construction-site
func newServiceMissingPointerField() *service { // want newServiceMissingPointerField:"never returns nil result 0"
	return &service{dep: realDoer{}, logger: &inner{}}
}

// Every field is nil, but each key is present — a presence-only checker reports
// nothing here.
//
//niloutofscope:new-service-explicit-nil why=construction-site
func newServiceExplicitNil() *service { // want newServiceExplicitNil:"never returns nil result 0"
	return &service{dep: nil, repo: nil, logger: nil}
}

// dep and logger are nil, and there is no composite literal to inspect.
//
//niloutofscope:new-service-by-assignment why=construction-site
func newServiceByAssignment() *service {
	s := new(service)
	s.repo = &inner{}

	return s
}

// repo is nil on a zero-valued struct.
//
//nilhazard:zero-value-struct sites=1
func zeroValueStruct() int {
	var s service

	return s.repo.n // want "s\\.repo may be nil here"
}

// ---------------------------------------------------------------------------
// B. Dereference of a pointer-typed field, in every statement form.
// Same hazard throughout: o.p is never guarded. A tool covering only the
// common forms will miss the rest.
// ---------------------------------------------------------------------------

//nilhazard:deref-return sites=1
func derefReturn(o *outer) int { return o.p.n } // want "o\\.p may be nil here"

//nilhazard:deref-assign-rhs sites=1
func derefAssignRHS(o *outer) int {
	x := o.p.n // want "o\\.p may be nil here"

	return x
}

//nilhazard:deref-assign-target sites=1
func derefAssignTarget(o *outer) { o.p.n = 1 } // want "o\\.p may be nil here"

//nilhazard:deref-if-cond sites=1
func derefIfCond(o *outer) bool {
	if o.p.n > 0 { // want "o\\.p may be nil here"
		return true
	}

	return false
}

//nilhazard:deref-for-cond sites=1
func derefForCond(o *outer) int {
	i := 0
	for i < o.p.n { // want "o\\.p may be nil here"
		i++
	}

	return i
}

//nilhazard:deref-for-post sites=1
func derefForPost(o *outer) int {
	c := 0
	for i := 0; i < 3; i += o.p.n { // want "o\\.p may be nil here"
		c++
	}

	return c
}

//nilhazard:deref-range sites=1
func derefRange(o *outer) int {
	total := 0
	for _, v := range o.p.items { // want "o\\.p may be nil here"
		total += v
	}

	return total
}

//nilhazard:deref-switch-subject sites=1
func derefSwitchSubject(o *outer) string {
	switch o.p.n { // want "o\\.p may be nil here"
	case 1:
		return "one"
	}

	return ""
}

//nilhazard:deref-switch-body sites=1
func derefSwitchBody(o *outer, k int) int {
	switch k {
	case 1:
		return o.p.n // want "o\\.p may be nil here"
	}

	return 0
}

//nilhazard:deref-defer sites=1
func derefDefer(o *outer) { defer o.p.touch() } // want "o\\.p may be nil here"

//nilhazard:deref-go sites=1
func derefGo(o *outer) { go o.p.touch() } // want "o\\.p may be nil here"

//nilhazard:deref-chan-send sites=1
func derefChanSend(o *outer, ch chan int) { ch <- o.p.n } // want "o\\.p may be nil here"

//nilhazard:deref-select sites=1
func derefSelect(o *outer, ch chan int) {
	select {
	case ch <- o.p.n: // want "o\\.p may be nil here"
	}
}

//nilhazard:deref-increment sites=1
func derefIncrement(o *outer) { o.p.n++ } // want "o\\.p may be nil here"

//nilhazard:deref-index sites=1
func derefIndex(o *outer) int { return o.p.items[0] } // want "o\\.p may be nil here"

//nilhazard:deref-slice-expr sites=1
func derefSliceExpr(o *outer) []int { return o.p.items[0:1] } // want "o\\.p may be nil here"

//nilhazard:deref-call-argument sites=1
func derefCallArgument(o *outer) int { return abs(o.p.n) } // want "o\\.p may be nil here"

//nilhazard:deref-method-on-field sites=1
func derefMethodOnField(o *outer) int { return o.p.value() } // want "o\\.p may be nil here"

//nilhazard:deref-field-channel sites=1
func derefFieldChannel(o *outer) { o.p.ch <- 1 } // want "o\\.p may be nil here"

// o.p and o.p.next.
//
//nilhazard:deref-chain sites=2
func derefChain(o *outer) int { return o.p.next.n } // want "o\\.p may be nil here" "o\\.p\\.next may be nil here"

// ---------------------------------------------------------------------------
// C. Guarded. Every one of these is correct — a report here is a false positive.
// ---------------------------------------------------------------------------

//nilsafe:guarded-if
func guardedIf(o *outer) int {
	if o.p != nil {
		return o.p.n
	}

	return 0
}

//nilsafe:guarded-early-return
func guardedEarlyReturn(o *outer) int {
	if o.p == nil {
		return 0
	}

	return o.p.n
}

//nilsafe:guarded-panic
func guardedPanic(o *outer) int {
	if o.p == nil {
		panic("p is required")
	}

	return o.p.n
}

//nilsafe:guarded-and-short-circuit
func guardedAndShortCircuit(o *outer) bool { return o.p != nil && o.p.n > 0 }

//nilsafe:guarded-or-short-circuit
func guardedOrShortCircuit(o *outer) bool { return o.p == nil || o.p.n > 0 }

//nilsafe:guarded-negated
func guardedNegated(o *outer) int {
	if !(o.p == nil) {
		return o.p.n
	}

	return 0
}

//nilsafe:guarded-by-assignment
func guardedByAssignment(o *outer) int {
	o.p = &inner{}

	return o.p.n
}

// The helper panics when the condition is false, so the field is non-nil below
// it. Proving this requires knowing what assert does.
//
//nilsafe:guarded-by-assert
func guardedByAssert(o *outer) int {
	assert(o.p != nil, "p is required")

	return o.p.n
}

// The same proof, through a helper in another package. A tool told which
// package holds the assert helper can prove this one without inlining it.
//
//nilsafe:guarded-by-imported-assert
func guardedByImportedAssert(o *outer) int {
	utility.Assert(o.p != nil, "p is required")

	return o.p.n
}

// validate returns an error unless every field is set. Proving this requires
// carrying a fact out of another function, and across packages when the
// validator is defined elsewhere.
//
//nilsafe:guarded-by-validator
func guardedByValidator(o *outer) int {
	if err := o.validate(); err != nil {
		return 0
	}

	return o.p.n
}

// A tagless switch whose first case returns on nil is a guard.
//
//nilsafe:guarded-switch-nil-case
func guardedSwitchNilCase(o *outer) int {
	switch {
	case o.p == nil:
		return 0
	default:
		return o.p.n
	}
}

// ---------------------------------------------------------------------------
// D. Interfaces.
// ---------------------------------------------------------------------------

//nilhazard:nil-interface-call sites=1
func nilInterfaceCall(o *outer) int { return o.iface.Do() } // want "o\\.iface may be nil here"

// The interface value is non-nil — it carries a type — while the pointer inside
// it is nil, so `d != nil` is true and Do still panics.
//
//nilhazard:typed-nil-in-interface sites=1
func typedNilInInterface() int {
	var p *ptrDoer

	var d Doer = p

	return d.Do() // want "d holds a nil pointer here"
}

//nilhazard:unchecked-type-assert sites=1
func uncheckedTypeAssert(v any) int { return v.(*inner).n } // want "v\\.\\(\\*inner\\) may be nil here"

//nilsafe:checked-type-assert
func checkedTypeAssert(v any) int {
	i, ok := v.(*inner)
	if !ok || i == nil {
		return 0
	}

	return i.n
}

//nilsafe:type-switch-nil
func typeSwitchNil(v any) int {
	switch t := v.(type) {
	case *inner:
		if t == nil {
			return 0
		}

		return t.n
	}

	return 0
}

// ---------------------------------------------------------------------------
// E. Across function boundaries. Nothing in the dereferencing function is
// wrong on its own — the nil is produced elsewhere.
// ---------------------------------------------------------------------------

//nilfixture:shared
func mayReturnNil(ok bool) *inner { // want mayReturnNil:"may return nil result 0"
	if ok {
		return &inner{}
	}

	return nil
}

//nilhazard:deref-func-result sites=1
func derefFuncResult() int { return mayReturnNil(false).n } // want "mayReturnNil\\(false\\) may be nil here"

// (nil, nil) is a not-found return, so a nil error does not imply a value.
// Recognizing that shape is nil-value analysis, not a pointer/interface
// dereference; the nilResults fact is still exported so the caller-side
// dereference below is caught.
//
//niloutofscope:find-or-nil why=not-a-dereference
func findOrNil(id int) (*inner, error) { // want findOrNil:"may return nil result 0"
	if id == 0 {
		return nil, nil
	}

	return &inner{}, nil
}

//nilhazard:deref-not-found-result sites=1
func derefNotFoundResult() int {
	v, err := findOrNil(0)
	if err != nil {
		return 0
	}

	return v.n // want "v may be nil here"
}

//nilfixture:shared
func newServiceOrErr(ok bool) (*service, error) {
	if !ok {
		return nil, errConstruct
	}

	return &service{dep: realDoer{}, repo: &inner{}, logger: &inner{}}, nil
}

//nilhazard:deref-ignoring-constructor-error sites=1
func derefIgnoringConstructorError() int {
	s, err := newServiceOrErr(false)
	_ = err

	return s.repo.n // want "s\\.repo may be nil here"
}

// ---------------------------------------------------------------------------
// F. Maps, slices, and the pointers inside them.
// ---------------------------------------------------------------------------

// Reading a nil map yields the zero value.
//
//nilsafe:nil-map-read
func nilMapRead() int {
	var m map[string]int

	return m["k"]
}

// Writing to a nil map panics. That is a nil MAP hazard, not a nil pointer or
// nil interface dereference.
//
//niloutofscope:nil-map-write why=not-a-dereference
func nilMapWrite() {
	var m map[string]int
	m["k"] = 1
}

// o.p. o.p.m is a nil map write, out of scope the same way nilMapWrite is.
//
//nilhazard:nil-map-write-on-field sites=1
func nilMapWriteOnField(o *outer) { o.p.m["k"] = 1 } // want "o\\.p may be nil here"

// Indexing a nil slice panics. That is a nil SLICE hazard, not a nil pointer
// or nil interface dereference.
//
//niloutofscope:nil-slice-index why=not-a-dereference
func nilSliceIndex() int {
	var s []int

	return s[0]
}

// Appending to a nil slice allocates.
//
//nilsafe:nil-slice-append
func nilSliceAppend() []int {
	var s []int

	return append(s, 1)
}

//nilhazard:missing-map-pointer-value sites=1
func missingMapPointerValue(m map[string]*inner) int { return m["k"].n } // want "m\\[\"k\"\\] may be nil here"

//nilhazard:nil-element-in-slice sites=1
func nilElementInSlice(s []*inner) int { return s[0].n } // want "s\\[0\\] may be nil here"

// ---------------------------------------------------------------------------
// G. Receivers and embedding.
// ---------------------------------------------------------------------------

// The method body dereferences the receiver.
//
//nilhazard:call-on-nil-receiver sites=1
func callOnNilReceiver() int {
	var i *inner

	return i.unsafeOnNil() // want "i is nil here"
}

// A nil receiver is a legal argument.
//
//nilsafe:call-safe-on-nil-receiver
func callSafeOnNilReceiver() bool {
	var i *inner

	return i.safeOnNil()
}

// A method that only calls another nil-safe method on its own receiver is
// nil-safe by delegation, not just the methods that guard directly.
//
//nilsafe:call-delegating-nil-safe-on-local
func callDelegatingNilSafeOnLocal() bool {
	var i *inner

	return i.delegating()
}

// Nil-safety by delegation travels through a nil-origin receiver too, not only
// a bare local whose nil origin was recorded in scope.
//
//nilsafe:call-nil-safe-on-call-result
func callNilSafeOnCallResult() bool {
	return mayReturnNil(false).safeOnNil()
}

// The near miss: a nil-origin call result is safe only for the specific method
// that proves it tolerates a nil receiver, not for any call reached through it.
//
//nilhazard:call-unsafe-on-call-result sites=1
func callUnsafeOnCallResult() int {
	return mayReturnNil(false).unsafeOnNil() // want "mayReturnNil\\(false\\) may be nil here"
}

// A field selection through a nil-origin expression is still a dereference,
// nil-safe-receiver fact or not.
//
//nilhazard:field-on-call-result sites=1
func fieldOnCallResult() int {
	return mayReturnNil(false).n // want "mayReturnNil\\(false\\) may be nil here"
}

// The embedded pointer is nil, and the promoted field reads through it.
//
//nilhazard:promoted-field-on-nil-embedded sites=1
func promotedFieldOnNilEmbedded(o *outer) int { return o.e } // want "o\\.embedded may be nil here"

// ---------------------------------------------------------------------------
// H. Concurrency. A guard in the enclosing function does not reach the closure.
// ---------------------------------------------------------------------------

// Blocks forever rather than panicking. That is a nil CHANNEL hazard, not a
// nil pointer or nil interface dereference.
//
//niloutofscope:nil-channel-send why=not-a-dereference
func nilChannelSend() {
	var ch chan int
	ch <- 1
}

// Panics. Still a nil channel hazard, not a pointer/interface dereference.
//
//niloutofscope:nil-channel-close why=not-a-dereference
func nilChannelClose() {
	var ch chan int
	close(ch)
}

//nilhazard:deref-in-goroutine sites=1
func derefInGoroutine(o *outer) {
	go func() {
		_ = o.p.n // want "o\\.p may be nil here"
	}()
}

// The guard holds when the goroutine is created, not when it runs, and another
// goroutine may set o.p to nil in between.
//
//nilhazard:guard-then-goroutine sites=1
func guardThenGoroutine(o *outer) {
	if o.p == nil {
		return
	}

	go func() {
		_ = o.p.n // want "o\\.p may be nil here"
	}()
}

// ---------------------------------------------------------------------------
// I. Nil errors and nil funcs.
// ---------------------------------------------------------------------------

// The caller cannot tell empty from absent. Recognizing that shape is
// nil-value analysis, not a pointer/interface dereference.
//
//niloutofscope:return-nil-nil why=not-a-dereference
func returnNilNil() (*inner, error) { return nil, nil } // want returnNilNil:"may return nil result 0"

// The error is checked and then discarded. Whether a checked error is
// propagated is not a question about a nil pointer or interface being
// dereferenced.
//
//niloutofscope:swallow-error why=not-nil-analysis
func swallowError() error {
	if err := doWork(); err != nil {
		return nil
	}

	return nil
}

// err may be nil.
//
//nilhazard:nil-error-message sites=1
func nilErrorMessage(err error) string { return err.Error() } // want "err may be nil here"

// o.p. The fn field call itself is a nil FUNC hazard, out of scope.
//
//nilhazard:call-nil-func-field sites=1
func callNilFuncField(o *outer) int { return o.p.fn() } // want "o\\.p may be nil here"

// Calling a nil func value is a nil FUNC hazard, not a nil pointer or nil
// interface dereference.
//
//niloutofscope:call-nil-func-var why=not-a-dereference
func callNilFuncVar() int {
	var f func() int

	return f()
}

// Always true. A declared function is never nil, so this comparison is not
// about a nil value reaching a dereference at all.
//
//niloutofscope:compare-declared-func-to-nil why=not-nil-analysis
func compareDeclaredFuncToNil() bool { return abs != nil }

// ---------------------------------------------------------------------------
// J. Guards that do not hold where the dereference is.
// ---------------------------------------------------------------------------

// The later check contradicts the deref.
//
//nilhazard:deref-then-nil-check sites=1
func derefThenNilCheck(o *outer) int {
	n := o.p.n // want "o\\.p may be nil here"
	if o.p == nil {
		return 0
	}

	return n
}

// The guard is about the old o.
//
//nilhazard:guard-then-reassign sites=1
func guardThenReassign(o *outer, other *outer) int {
	if o.p == nil {
		return 0
	}

	o = other

	return o.p.n // want "other\\.p may be nil here"
}

// The guard only runs when k is true.
//
//nilhazard:guard-out-of-scope sites=1
func guardOutOfScope(o *outer, k bool) int {
	if k {
		if o.p == nil {
			return 0
		}
	}

	return o.p.n // want "o\\.p may be nil here"
}

// The loop may store nil.
//
//nilhazard:guard-then-loop-reassign sites=1
func guardThenLoopReassign(o *outer, list []*inner) int {
	if o.p == nil {
		return 0
	}

	for _, v := range list {
		o.p = v
	}

	return o.p.n // want "o\\.p may be nil here"
}

// ---------------------------------------------------------------------------
// K. Indirection.
// ---------------------------------------------------------------------------

// pp, and *pp.
//
//nilhazard:double-pointer sites=2
func doublePointer(pp **inner) int { return (*pp).n } // want "pp may be nil here" "\\(\\*pp\\) may be nil here"

// pp, *pp, and the next field.
//
//nilhazard:double-pointer-chain sites=3
func doublePointerChain(pp **inner) int { return (*pp).next.n } // want "pp may be nil here" "\\(\\*pp\\) may be nil here" "\\(\\*pp\\)\\.next may be nil here"

//nilhazard:nil-inside-value-struct sites=1
func nilInsideValueStruct() int {
	v := outer{}

	return v.p.n // want "v\\.p may be nil here"
}
