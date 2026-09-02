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

func must(ok bool) { // want must:"asserts argument 0"
	if !ok {
		panic("must")
	}
}

func check(msg string, ok bool) { // want check:"asserts argument 1"
	if ok {
		return
	}

	panic(msg)
}

func die(msg string) { // want die:"never returns"
	panic(msg)
}

func ensure(ok bool, msg string) { // want ensure:"asserts argument 0"
	if !ok {
		die(msg)
	}
}

func provenByLocalMust(o *outer) int {
	must(o.p != nil)

	return o.p.n
}

func provenBySecondArgument(o *outer) int {
	check("p", o.p != nil)

	return o.p.n
}

func provenThroughNeverReturnsChain(o *outer) int {
	ensure(o.p != nil, "p")

	return o.p.n
}

func mustWithTrace(ok bool, msg string) { // want mustWithTrace:"asserts argument 0"
	if ok {
		return
	}

	trace := msg + "!"
	panic(trace)
}

func provenByAssertBuildingItsPanic(o *outer) int {
	mustWithTrace(o.p != nil, "p")

	return o.p.n
}
