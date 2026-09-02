package composite

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

func provenBySingleLevelLiteral() int {
	o := &outer{p: &inner{}}

	return o.p.n
}

func provenByNestedLiteral() int {
	o := &outer{p: &inner{q: &deep{}}}

	return o.p.q.n
}

func fieldNotSetInLiteral() int {
	o := &outer{}

	return o.p.n // want "o\\.p may be nil here"
}

func fieldReassignedAfterLiteral(other *inner) int {
	o := &outer{p: &inner{}}

	o.p = other

	return o.p.n // want "o\\.p may be nil here"
}
