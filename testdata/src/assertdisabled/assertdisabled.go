package assertdisabled

import "assertpkg/utility"

type inner struct {
	n int
}

type outer struct {
	p *inner
}

// With no assert_package configured the helper proves nothing, so the same code that
// passes in assertproof is reported here.
func assertProvesNothing(o *outer) int {
	utility.Assert(o.p != nil, "p must be set")

	return o.p.n // want "o\\.p may be nil here"
}
