package analyzer

import "strconv"

// assertHelper marks a function that panics unless its Arg-th argument is true.
type assertHelper struct{ Arg int }

func (*assertHelper) AFact() {}

func (f *assertHelper) String() string { return "asserts argument " + strconv.Itoa(f.Arg) }

// neverReturns marks a function whose body cannot complete normally.
type neverReturns struct{}

func (*neverReturns) AFact() {}

func (*neverReturns) String() string { return "never returns" }
