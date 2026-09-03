package guards

import "errors"

type inner struct {
	n  int
	fn func() int
}

type embedded struct {
	e int
}

type outer struct {
	*embedded

	p *inner
	q *inner
}

func earlyReturn(o *outer) int {
	if o.p == nil {
		return 0
	}

	return o.p.n
}

func earlyPanic(o *outer) int {
	if o.p == nil {
		panic("nil")
	}

	return o.p.n
}

func positiveBranch(o *outer) int {
	if o.p != nil {
		return o.p.n
	}

	return 0
}

func elseBranch(o *outer) int {
	if o.p == nil {
		return 0
	} else {
		return o.p.n
	}
}

func afterElseExits(o *outer) int {
	if o.p != nil {
	} else {
		return 0
	}

	return o.p.n
}

func andGuard(o *outer) int {
	if o.p != nil && o.p.n > 0 {
		return o.p.n
	}

	return 0
}

func andGuardChained(o *outer, other *outer) int {
	if o.p != nil && other.q != nil {
		return o.p.n + other.q.n
	}

	return 0
}

func orGuard(o *outer) int {
	if o.p == nil || o.p.n == 0 {
		return 0
	}

	return o.p.n
}

func negatedGuard(o *outer) int {
	if !(o.p == nil) {
		return o.p.n
	}

	return 0
}

func parenGuard(o *outer) int {
	if (o.p) != nil {
		return o.p.n
	}

	return 0
}

func aliasGuard(o *outer) int {
	p := o.p
	if p == nil {
		return 0
	}

	return o.p.n
}

func forCondGuard(o *outer) int {
	for o.p != nil && o.p.n > 0 {
		o.p.n--
	}

	return 0
}

func newValue(o *outer) int {
	o.p = &inner{n: 1}

	return o.p.n
}

func derefBeforeGuard(o *outer) int {
	if o.p.n > 0 && o.p != nil { // want "o\\.p may be nil here"
		return 1
	}

	return 0
}

func orIsNotAProof(o *outer, flag bool) int {
	if o.p != nil || flag {
		return o.p.n // want "o\\.p may be nil here"
	}

	return 0
}

func guardOnASiblingField(o *outer) int {
	if o.q == nil {
		return 0
	}

	return o.p.n // want "o\\.p may be nil here"
}

func fallthroughAfterIf(o *outer) int {
	if o.p != nil {
		_ = o.p.n
	}

	return o.p.n // want "o\\.p may be nil here"
}

func switchNilCaseReturns(o *outer) int {
	switch {
	case o.p == nil:
		return 0
	}

	return o.p.n
}

func switchDefaultProven(o *outer) int {
	switch {
	case o.p == nil:
		return 0
	default:
		return o.p.n
	}
}

func switchLaterCaseProven(o *outer) int {
	switch {
	case o.q == nil:
		return 0
	case o.p == nil:
		return 1
	default:
		return o.p.n + o.q.n
	}
}

func switchNonExitingCaseProvesNothing(o *outer) int {
	switch {
	case o.p == nil:
		_ = o.p
	}

	return o.p.n // want "o\\.p may be nil here"
}

func switchTagNilCase(o *outer) int {
	switch o.p {
	case nil:
		return 0
	}

	return o.p.n
}

func switchMultiExprCase(o *outer) int {
	switch {
	case o.p == nil, o.q == nil:
		return 0
	}

	return o.p.n + o.q.n
}

func switchCaseDoesNotProveSibling(o *outer) int {
	switch {
	case o.p == nil:
		return 0
	default:
		return o.q.n // want "o\\.q may be nil here"
	}
}

func starGuard(p *int) int {
	if p == nil {
		return 0
	}

	return *p
}

func doublePointerGuard(pp **inner) int {
	if pp == nil || *pp == nil {
		return 0
	}

	return (*pp).n
}

func errorGuard(err error) string {
	if err != nil {
		return err.Error()
	}

	return ""
}

func funcFieldGuard(o *outer) int {
	if o.p != nil && o.p.fn != nil {
		return o.p.fn()
	}

	return 0
}

func embeddedGuard(o *outer) int {
	if o.embedded == nil {
		return 0
	}

	return o.e
}

// starUnguarded dereferences a parameter with no visible nil origin, which is
// the one bare shape this analyzer leaves alone: the same reason a selector
// through such a parameter stays silent.
func starUnguarded(p *int) int { return *p }

func errorUnguarded(err error) string { return err.Error() } // want "err may be nil here"

func declaredNilThenAssigned() int {
	var i *inner
	i = &inner{}

	return i.n
}

func declaredNilGuarded() int {
	var i *inner
	if i == nil {
		return 0
	}

	return i.n
}

func mapValueGuarded(m map[string]*inner) int {
	v := m["k"]
	if v == nil {
		return 0
	}

	return v.n
}

func mapValueUnguarded(m map[string]*inner) int {
	v := m["k"]

	return v.n // want "v may be nil here"
}

func sliceElementUnguardedLocal(s []*inner) int {
	v := s[0]

	return v.n // want "v may be nil here"
}

func nilAssignedLater(o *outer) int {
	p := o.p
	p = nil

	return p.n // want "p is nil here"
}

func typeAssertLocal(v any) int {
	i := v.(*inner)

	return i.n // want "i may be nil here"
}

func lookup(ok bool) *inner { // want lookup:"may return nil result 0"
	if ok {
		return &inner{}
	}

	return nil
}

func lookupGuarded() int {
	v := lookup(false)
	if v == nil {
		return 0
	}

	return v.n
}

func lookupUnguarded() int {
	v := lookup(false)

	return v.n // want "v may be nil here"
}

var errNotFound = errors.New("not found")

func find(ok bool) (*inner, error) {
	if !ok {
		return nil, errNotFound
	}

	return &inner{}, nil
}

func findChecked() int {
	v, err := find(false)
	if err != nil {
		return 0
	}

	return v.n
}

// The ok result is the check people write for a comma-ok assertion, so the value
// half is not tracked as a nil origin: only the single-form assertion is.
func commaOkAssertChecked(v any) int {
	i, ok := v.(*inner)
	if !ok {
		return 0
	}

	return i.n
}

func take(pp **inner) bool {
	*pp = &inner{}

	return true
}

// Passing &ce to a call hands the callee the variable, so the nil state known
// from its declaration no longer holds.
func addressTakenClearsNilState() int {
	var ce *inner
	if !take(&ce) {
		return 0
	}

	return ce.n
}

var errSentinel = errors.New("sentinel")

// A package-level variable is out of scope the same way a package-qualified one
// is, so the bare-error rule does not fire on a sentinel.
func sentinelMessage() string {
	return errSentinel.Error()
}
