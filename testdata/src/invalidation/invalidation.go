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
