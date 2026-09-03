// Package nonnilresults isolates the nonNilResults fact: which results a
// function proves non-nil on every return path, how that proof propagates
// through a wrapper getter, and how it feeds into the package-local field
// wiring rule for a singleton-getter constructor.
package nonnilresults

type T struct{ n int }

// getter returns only an address, so it qualifies for the fact.
func getter() *T { // want getter:"never returns nil result 0"
	return &T{}
}

// condGetter returns nil on one path, so it never qualifies for
// nonNilResults - it only qualifies for the pre-existing, unrelated
// nilResults fact, since the nil-yielding return is unconditional.
func condGetter(ok bool) *T { // want condGetter:"may return nil result 0"
	if ok {
		return &T{}
	}

	return nil
}

// namedGetter has a named result, which disqualifies it outright even
// though its only return yields an address.
func namedGetter() (result *T) {
	return &T{}
}

// wrapper returns the exact result of a proven getter, proving the fact is
// transitive through a wrapper.
func wrapper() *T { // want wrapper:"never returns nil result 0"
	return getter()
}

// unprovenWrapper returns the result of an unproven getter, so it does not
// qualify either.
func unprovenWrapper() *T {
	return condGetter(true)
}

// getterWithErr mirrors the corpus (*T, error) constructor shape: only its
// non-error result 0 is provably non-nil, and the error result must not
// appear in the fact.
func getterWithErr() (*T, error) { // want getterWithErr:"never returns nil result 0"
	return &T{}, nil
}

// holder wires its only pointer field from a proven getter, which is the
// shape the whole change exists for: the field is silently treated as
// wired, and the dereference below is not reported.
type holder struct {
	dep *T
}

func newHolder() *holder { // want newHolder:"never returns nil result 0"
	return &holder{dep: getter()}
}

func (h *holder) Use() int {
	return h.dep.n
}

// unprovenHolder wires the same field from an unproven getter, so the field
// stays unproven and the dereference is reported.
type unprovenHolder struct {
	dep *T
}

func newUnprovenHolder() *unprovenHolder { // want newUnprovenHolder:"never returns nil result 0"
	return &unprovenHolder{dep: condGetter(true)}
}

func (u *unprovenHolder) Use() int {
	return u.dep.n // want "u\\.dep may be nil here"
}
