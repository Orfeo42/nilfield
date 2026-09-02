package statements

type inner struct {
	n     int
	items []int
	v     any
}

type outer struct {
	p *inner
}

func (i *inner) close() {} // want close:"nil-safe receiver"

func plainReturn(o *outer) int {
	return o.p.n // want "o\\.p may be nil here"
}

func shortDecl(o *outer) int {
	x := o.p.n // want "o\\.p may be nil here"

	return x
}

func varDecl(o *outer) int {
	var x = o.p.n // want "o\\.p may be nil here"

	return x
}

func deferred(o *outer) {
	defer o.p.close() // want "o\\.p may be nil here"
}

func goroutine(o *outer) {
	go o.p.close() // want "o\\.p may be nil here"
}

func rangeHeader(o *outer) int {
	total := 0
	for _, v := range o.p.items { // want "o\\.p may be nil here"
		total += v
	}

	return total
}

func send(o *outer, c chan int) {
	c <- o.p.n // want "o\\.p may be nil here"
}

func incDec(o *outer) {
	o.p.n++ // want "o\\.p may be nil here"
}

func switchTag(o *outer) {
	switch o.p.n { // want "o\\.p may be nil here"
	case 1:
	}
}

func switchCaseExpr(o *outer, v int) {
	switch v {
	case o.p.n: // want "o\\.p may be nil here"
	}
}

func forHeader(o *outer) {
	for i := 0; i < o.p.n; i++ { // want "o\\.p may be nil here"
	}
}

func forPost(o *outer) {
	for i := 0; i < 3; i += o.p.n { // want "o\\.p may be nil here"
	}
}

func labeled(o *outer) {
loop:
	for {
		_ = o.p.n // want "o\\.p may be nil here"

		break loop
	}
}

func selectComm(o *outer, c chan int) {
	select {
	case c <- o.p.n: // want "o\\.p may be nil here"
	}
}

func selectBody(o *outer, c chan int) {
	select {
	case <-c:
		_ = o.p.n // want "o\\.p may be nil here"
	}
}

func typeSwitchBody(o *outer, x any) {
	switch x.(type) {
	case int:
		_ = o.p.n // want "o\\.p may be nil here"
	}
}

func typeSwitchSubject(o *outer) {
	switch o.p.v.(type) { // want "o\\.p may be nil here"
	case int:
	}
}

func assignToField(o *outer) {
	o.p.n = 1 // want "o\\.p may be nil here"
}
