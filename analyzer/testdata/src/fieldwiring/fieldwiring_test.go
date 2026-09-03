package fieldwiring

// partialTestOnlyPartial constructs testOnlyPartial without its dep field.
// It lives only in this _test.go file, so it must not count against
// testOnlyPartial's wiring: the field-wiring computation excludes test files
// the same way report time already does.
func partialTestOnlyPartial() *testOnlyPartial { // want partialTestOnlyPartial:"never returns nil result 0"
	return &testOnlyPartial{}
}
