package excluded

type inner struct {
	n int
}

type outer struct {
	p *inner
}

// Reported everywhere else; silent here because the test runs with this directory
// listed in exclude-paths.
func unguarded(o *outer) int {
	return o.p.n
}
