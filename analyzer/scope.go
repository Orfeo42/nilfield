package analyzer

import (
	"maps"
	"strings"
)

// nilState names what is known about a bare local's own value at a point in the
// function, as opposed to proven (which says a path is known NON-nil).
type nilState int

const (
	isNil    nilState = iota + 1 // "x is nil here"
	typedNil                     // "x holds a nil pointer here" (interface holding a nil pointer)
	maybeNil                     // "x may be nil here"
)

// message is the diagnostic suffix for state.
func (s nilState) message() string {
	switch s {
	case isNil:
		return "is nil here"
	case typedNil:
		return "holds a nil pointer here"
	case maybeNil:
		return "may be nil here"
	default:
		return "may be nil here"
	}
}

// scope carries the flow-sensitive state of one branch: which canonical paths are
// known non-nil, and which local names are known to alias which canonical path.
// errProof additionally maps a local error variable to the paths that are non-nil
// on the branch where that variable is nil, which is how a checked validator call
// hands its postcondition to the surrounding `if err != nil` guard. nilable maps a
// bare local NAME (never a field or star path) to what is known about its own
// value, which is what lets a dereference of that same local, later in the same
// function, be reported without a fact.
type scope struct {
	proven   map[string]bool
	alias    map[string]string
	errProof map[string][]string
	nilable  map[string]nilState
}

func newScope() scope {
	return scope{
		proven:   map[string]bool{},
		alias:    map[string]string{},
		errProof: map[string][]string{},
		nilable:  map[string]nilState{},
	}
}

func (s scope) clone() scope {
	proven := make(map[string]bool, len(s.proven))
	maps.Copy(proven, s.proven)

	alias := make(map[string]string, len(s.alias))
	maps.Copy(alias, s.alias)

	errProof := make(map[string][]string, len(s.errProof))
	maps.Copy(errProof, s.errProof)

	nilable := make(map[string]nilState, len(s.nilable))
	maps.Copy(nilable, s.nilable)

	return scope{proven: proven, alias: alias, errProof: errProof, nilable: nilable}
}

func (s scope) with(paths []string) scope {
	out := s.clone()
	for _, path := range paths {
		out.proven[path] = true
	}

	return out
}

// invalidate drops every proof and alias that the write to path could have falsified:
// the path itself, everything reachable through it, and any local aliasing either.
func (s scope) invalidate(path string) {
	delete(s.proven, path)
	delete(s.nilable, path)

	prefix := path + "."

	for k := range s.proven {
		if strings.HasPrefix(k, prefix) {
			delete(s.proven, k)
		}
	}

	for name, target := range s.alias {
		if target == path || strings.HasPrefix(target, prefix) {
			delete(s.alias, name)
		}
	}

	for name, paths := range s.errProof {
		kept := make([]string, 0, len(paths))

		for _, p := range paths {
			if p == path || strings.HasPrefix(p, prefix) {
				continue
			}

			kept = append(kept, p)
		}

		if len(kept) == 0 {
			delete(s.errProof, name)

			continue
		}

		s.errProof[name] = kept
	}
}

// markNil records what is known about the bare local named name's own value.
func (s scope) markNil(name string, state nilState) {
	s.nilable[name] = state
}

func (s scope) dropNil(name string) {
	delete(s.nilable, name)
}

// goroutineScope returns the scope a closure launched with `go` starts from. A
// guard proven before the `go` statement only holds at the moment the goroutine
// is scheduled, not while it runs — another goroutine can mutate the same field
// or dereference in between — so FIELD and STAR path proofs are dropped. A bare
// local proof is kept: nothing else can write to that local unless it is
// captured by reference elsewhere, which this analyzer does not track. An alias
// of an already-proven path was a snapshot taken under the guard and is dropped
// along with the proof it depended on (keeping it as a plain local would read as
// an unproven bare local and go silently unreported); an alias of an unproven
// path is kept so the local still resolves back to the original field path.
func goroutineScope(sc scope) scope {
	out := newScope()

	maps.Copy(out.nilable, sc.nilable)

	for p, proven := range sc.proven {
		if proven && !isFieldPath(p) && !isStarPath(p) {
			out.proven[p] = true
		}
	}

	for name, target := range sc.alias {
		if !sc.proven[target] {
			out.alias[name] = target
		}
	}

	return out
}
