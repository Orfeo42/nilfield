// Package nilpredicate isolates the nil-predicate rule: a bool-returning
// function whose body answers true when its argument is nil proves that
// argument non-nil on the branch where the call is false.
package nilpredicate

type inner struct {
	name string
}

// isNil is the shape the rule derives from: a leading `== nil` guard
// answering true, with whatever follows deciding the rest.
func isNil(v any) bool { // want isNil:"reports nil argument 0"
	if v == nil {
		return true
	}

	_, isInner := v.(*inner)

	return !isInner
}

// wrapIsNil forwards to isNil and inherits its answer for the same argument.
func wrapIsNil(v any) bool { // want wrapIsNil:"reports nil argument 0"
	return isNil(v)
}

// noGuard answers by shape alone and never states anything about nil, so it
// proves nothing.
func noGuard(v any) bool {
	_, isInner := v.(*inner)

	return isInner
}

// guardAfterWork has the right guard but reaches it too late: the statement
// before it can already answer for a nil argument.
func guardAfterWork(v any) bool {
	if _, isInner := v.(*inner); isInner {
		return false
	}

	if v == nil {
		return true
	}

	return false
}

func negatedCallProves(err error) string {
	if !isNil(err) {
		return err.Error()
	}

	return ""
}

func wrappedCallProves(err error) string {
	if !wrapIsNil(err) {
		return err.Error()
	}

	return ""
}

func trueBranchProvesNothing(err error) string {
	if isNil(err) {
		return err.Error() // want "err may be nil here"
	}

	return ""
}

func elseBranchProves(err error) string {
	if isNil(err) {
		return ""
	}

	return err.Error()
}

func noGuardProvesNothing(err error) string {
	if !noGuard(err) {
		return err.Error() // want "err may be nil here"
	}

	return ""
}

func guardAfterWorkProvesNothing(err error) string {
	if !guardAfterWork(err) {
		return err.Error() // want "err may be nil here"
	}

	return ""
}

func fieldProvenByPredicate(o *outer) string {
	if !isNil(o.in) {
		return o.in.name
	}

	return ""
}

type outer struct {
	in *inner
}
