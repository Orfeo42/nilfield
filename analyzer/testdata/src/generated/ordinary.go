package generated

type ordinaryOuter struct {
	p *inner
}

func unguardedOrdinary(o *ordinaryOuter) int {
	return o.p.n // want "o\\.p may be nil here"
}
