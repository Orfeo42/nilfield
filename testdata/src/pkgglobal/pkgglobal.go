package pkgglobal

import "pkgglobal_dep"

type inner struct {
	n int
}

type outer struct {
	p *inner
}

func readsGlobal() int {
	return pkgglobal_dep.GlobalPtr.N
}

func readsStructField(o *outer) int {
	return o.p.n // want "o\\.p may be nil here"
}
