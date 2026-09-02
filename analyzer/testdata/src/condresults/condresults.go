package condresults

type entity struct {
	n int
}

type entityWithOwner struct {
	entity
	owner string
}

type dto struct {
	N int
}

// toDto returns nil only when its own parameter is nil.
func toDto(e *entity) *dto { // want toDto:"may return nil result 0 when param 0 is nil"
	if e == nil {
		return nil
	}

	return &dto{N: e.n}
}

// toDtoWithErr is the same shape as toDto behind an (X, error) signature, to
// exercise the multi-result assignment path.
func toDtoWithErr(e *entity) (*dto, error) { // want toDtoWithErr:"may return nil result 0 when param 0 is nil"
	if e == nil {
		return nil, nil
	}

	return &dto{N: e.n}, nil
}

// unconditionalMapper's nil return is conditioned on a bool, not a nil
// parameter, so it must keep being reported unconditionally.
func unconditionalMapper(ok bool) *dto { // want unconditionalMapper:"may return nil result 0"
	if !ok {
		return nil
	}

	return &dto{}
}

// mixedGuardMapper has two nil-yielding returns: only one is guarded by a nil
// parameter check, so the result must stay unconditional.
func mixedGuardMapper(e *entity, ok bool) *dto { // want mixedGuardMapper:"may return nil result 0"
	if e == nil {
		return nil
	}

	if !ok {
		return nil
	}

	return &dto{N: e.n}
}

// guardedOnDifferentParam's nil return is guarded by a check on "other", not
// on "e", so an address passed for "e" does not silence the call site.
func guardedOnDifferentParam(e *entity, other *entity) *dto { // want guardedOnDifferentParam:"may return nil result 0 when param 1 is nil"
	if other == nil {
		return nil
	}

	return &dto{N: e.n}
}

// silentWithAddress calls toDto with a composite-literal address, which
// isDefinitelyNonNil proves non-nil, so the conditional result is silent.
func silentWithAddress(ew *entityWithOwner) int {
	d := toDto(&ew.entity)

	return d.N
}

// reportedWithUnknownArg calls toDto with a plain parameter of unknown
// provenance, so the conditional result stays nilable and is reported.
func reportedWithUnknownArg(p *entity) int {
	d := toDto(p)

	return d.N // want "d may be nil here"
}

// silentWithProvenArg calls toDto with a parameter a `!= nil` guard proved
// non-nil earlier in the function, so the conditional result is silent.
func silentWithProvenArg(p *entity) int {
	if p == nil {
		return 0
	}

	d := toDto(p)

	return d.N
}

// silentWithAddressExprForm exercises the expression-form consumption site
// (checkNilOriginBase) instead of an intermediate assignment.
func silentWithAddressExprForm(ew *entityWithOwner) int {
	return toDto(&ew.entity).N
}

// reportedWithUnknownArgExprForm is reportedWithUnknownArg's expression-form
// counterpart.
func reportedWithUnknownArgExprForm(p *entity) int {
	return toDto(p).N // want "toDto\\(p\\) may be nil here"
}

// reportedFromTupleAssignment exercises markNilResultsFromCall, the
// multi-result assignment path, with an unproven argument.
func reportedFromTupleAssignment(p *entity) int {
	d, _ := toDtoWithErr(p)

	return d.N // want "d may be nil here"
}

// silentFromTupleAssignment is reportedFromTupleAssignment's counterpart with
// a provably non-nil address argument.
func silentFromTupleAssignment(ew *entityWithOwner) int {
	d, _ := toDtoWithErr(&ew.entity)

	return d.N
}

// reportedRegardlessOfArg shows a bool-guarded nil result is reported
// regardless of the argument, since no parameter conditions it.
func reportedRegardlessOfArg() int {
	d := unconditionalMapper(true)

	return d.N // want "d may be nil here"
}

// reportedRegardlessOfArgExprForm is the expression-form counterpart of
// reportedRegardlessOfArg, mirroring the corpus's mayReturnNil(ok bool) shape.
func reportedRegardlessOfArgExprForm() int {
	return unconditionalMapper(true).N // want "unconditionalMapper\\(true\\) may be nil here"
}

// reportedDespiteMixedGuard shows a result left unconditional by a mixed guard
// is still reported even when the guarded parameter's argument is an address.
func reportedDespiteMixedGuard(ew *entityWithOwner) int {
	d := mixedGuardMapper(&ew.entity, true)

	return d.N // want "d may be nil here"
}

// reportedWhenGuardedParamUnproven shows the guard tracks the parameter it
// actually checks ("other"), not the one passed as an address ("e").
func reportedWhenGuardedParamUnproven(ew *entityWithOwner, other *entity) int {
	d := guardedOnDifferentParam(&ew.entity, other)

	return d.N // want "d may be nil here"
}
