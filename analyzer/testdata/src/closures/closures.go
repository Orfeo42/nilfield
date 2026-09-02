package closures

type inner struct {
	n int
}

type outer struct {
	p *inner
}

func guardedClosure(o *outer) {
	if o.p != nil {
		func() {
			_ = o.p.n
		}()
	}
}

func guardedGoroutine(o *outer) {
	if o.p != nil {
		go func() {
			_ = o.p.n // want "o\\.p may be nil here"
		}()
	}
}

func guardedDeferredClosure(o *outer) {
	if o.p != nil {
		defer func() {
			_ = o.p.n
		}()
	}
}

func aliasSnapshotInGoroutine(o *outer) {
	p := o.p
	if p == nil {
		return
	}

	go func() {
		_ = p.n
	}()
}

func aliasUnprovenInGoroutine(o *outer) {
	p := o.p

	go func() {
		_ = p.n // want "o\\.p may be nil here"
	}()
}

func knownNilLocalInGoroutine() {
	var i *inner

	go func() {
		_ = i.n // want "i is nil here"
	}()
}

func goroutineArgumentEvaluatedUnderGuard(o *outer) {
	if o.p == nil {
		return
	}

	go func(n int) {
		_ = n
	}(o.p.n)
}

func unguardedClosure(o *outer) {
	go func() {
		_ = o.p.n // want "o\\.p may be nil here"
	}()
}

var packageLevelFunc = func(o *outer) int {
	return o.p.n // want "o\\.p may be nil here"
}
