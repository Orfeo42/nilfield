package invalidation

type deep struct {
	n int
}

type inner struct {
	n int
	q *deep
}

type outer struct {
	p *inner
}

func selectorAssignedNil(o *outer) int {
	if o.p == nil {
		return 0
	}

	o.p = nil

	return o.p.n // want "o\\.p may be nil here"
}

func selectorReassigned(o *outer, other *inner) int {
	if o.p == nil {
		return 0
	}

	o.p = other

	return o.p.n // want "o\\.p may be nil here"
}

func nestedProofDropped(o *outer) int {
	if o.p == nil || o.p.q == nil {
		return 0
	}

	o.p = &inner{}

	return o.p.q.n // want "o\\.p\\.q may be nil here"
}

func multiAssign(o *outer, other *inner) int {
	if o.p == nil {
		return 0
	}

	o.p, other = nil, other

	return o.p.n // want "o\\.p may be nil here"
}

func loopBodyInvalidatesGuard(o *outer) {
	if o.p == nil {
		return
	}

	for i := 0; i < 3; i++ {
		_ = o.p.n // want "o\\.p may be nil here"

		o.p = nil
	}
}

// The local still holds the value read before the field was cleared, so nothing is
// reported here: only paths reached through a struct are in scope.
func aliasSurvivesFieldWrite(o *outer) int {
	p := o.p
	if p == nil {
		return 0
	}

	o.p = nil

	return p.n
}

func bareBlockInvalidatesOuterProof(o *outer) int {
	if o.p == nil {
		return 0
	}

	{
		o.p = nil
	}

	return o.p.n // want "o\\.p may be nil here"
}

func reassignedProofStillHolds(o *outer) int {
	if o.p == nil {
		return 0
	}

	o.p = &inner{}

	return o.p.n
}

func loopWriteInvalidatesAfterLoop(o *outer, list []*inner) int {
	if o.p == nil {
		return 0
	}

	for _, v := range list {
		o.p = v
	}

	return o.p.n // want "o\\.p may be nil here"
}

func forLoopWriteInvalidatesAfterLoop(o *outer) int {
	if o.p == nil {
		return 0
	}

	for i := 0; i < 3; i++ {
		o.p = nil
	}

	return o.p.n // want "o\\.p may be nil here"
}

func ifBranchWriteInvalidatesAfterIf(o *outer, k bool) int {
	if o.p == nil {
		return 0
	}

	if k {
		o.p = nil
	}

	return o.p.n // want "o\\.p may be nil here"
}

func elseBranchWriteInvalidatesAfterIf(o *outer, k bool) int {
	if o.p == nil {
		return 0
	}

	if k {
		_ = o.p.n
	} else {
		o.p = nil
	}

	return o.p.n // want "o\\.p may be nil here"
}

func exitingBranchWriteDoesNotInvalidate(o *outer, k bool) int {
	if o.p == nil {
		return 0
	}

	if k {
		o.p = nil
		return 1
	}

	return o.p.n
}

func switchClauseWriteInvalidatesAfterSwitch(o *outer, k int) int {
	if o.p == nil {
		return 0
	}

	switch k {
	case 1:
		o.p = nil
	}

	return o.p.n // want "o\\.p may be nil here"
}
