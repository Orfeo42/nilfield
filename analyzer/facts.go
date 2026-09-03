package analyzer

import (
	"strconv"
	"strings"
)

// assertHelper marks a function that panics unless its Arg-th argument is true.
type assertHelper struct{ Arg int }

func (*assertHelper) AFact() {}

func (f *assertHelper) String() string { return "asserts argument " + strconv.Itoa(f.Arg) }

// neverReturns marks a function whose body cannot complete normally.
type neverReturns struct{}

func (*neverReturns) AFact() {}

func (*neverReturns) String() string { return "never returns" }

// nonNilResults marks a function whose named results are proven non-nil on
// every return path, computed and exported by exportNonNilResultsFact.
// Results lists the zero-based indexes that qualify.
type nonNilResults struct{ Results []int }

func (*nonNilResults) AFact() {}

func (f *nonNilResults) String() string {
	parts := make([]string, 0, len(f.Results))

	for _, r := range f.Results {
		parts = append(parts, "never returns nil result "+strconv.Itoa(r))
	}

	return strings.Join(parts, ", ")
}
