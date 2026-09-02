package closures

type inner struct {
	n int
}

type outer struct {
	p *inner
}

func guardedClosure(o *outer) {
	if o.p != nil {
		go func() {
			_ = o.p.n
		}()
	}
}

func unguardedClosure(o *outer) {
	go func() {
		_ = o.p.n // want "o\\.p may be nil here"
	}()
}

var packageLevelFunc = func(o *outer) int {
	return o.p.n // want "o\\.p may be nil here"
}
