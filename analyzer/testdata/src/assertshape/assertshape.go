package assertshape

type inner struct {
	n int
}

type outer struct {
	p *inner
}

func note(cond bool, msg string) {
	_ = msg
}

func softAssert(cond bool) {
	if !cond {
		return
	}
}

func logOnly(cond bool) {
	if !cond {
		println("x")
	}
}

func provenByNote(o *outer) int {
	note(o.p != nil, "p must be set")

	return o.p.n // want "o\\.p may be nil here"
}

func provenBySoftAssert(o *outer) int {
	softAssert(o.p != nil)

	return o.p.n // want "o\\.p may be nil here"
}

func provenByLogOnly(o *outer) int {
	logOnly(o.p != nil)

	return o.p.n // want "o\\.p may be nil here"
}
