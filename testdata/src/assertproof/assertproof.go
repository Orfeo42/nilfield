package assertproof

import "assertpkg/utility"

type inner struct {
	n int
}

type outer struct {
	p *inner
	q *inner
}

func provenByAssert(o *outer) int {
	utility.Assert(o.p != nil, "p must be set")

	return o.p.n
}

func provenByAssertWithCode(o *outer) int {
	utility.AssertWithCode(o.p != nil, 400, "p must be set")

	return o.p.n
}

func provenByAssertConjunction(o *outer) int {
	utility.Assert(o.p != nil && o.q != nil, "both must be set")

	return o.p.n + o.q.n
}

func assertOnAnotherField(o *outer) int {
	utility.Assert(o.q != nil, "q must be set")

	return o.p.n // want "o\\.p may be nil here"
}
