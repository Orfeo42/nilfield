package guards

type inner struct {
	n int
}

type outer struct {
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
