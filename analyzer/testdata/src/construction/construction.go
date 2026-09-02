// Package construction is a corpus for the construction-site rule: a wiring
// struct - one whose fields are all pointers or interfaces - built with some of
// those fields left nil.
package construction

type dep interface {
	Do()
}

type impl struct{}

func (impl) Do() {}

type unit struct {
	n int
}

type wiring struct {
	d dep
	a *unit
	b *unit
}

type mixed struct {
	a *unit
	n int
}

func partialLiteral() *wiring {
	return &wiring{a: &unit{}} // want "wiring is constructed with d, b left nil"
}

func explicitNilField() wiring {
	return wiring{d: nil, a: &unit{}, b: &unit{}} // want "wiring is constructed with d left nil"
}

func emptyLiteral() *wiring {
	return &wiring{}
}

func fullLiteral() *wiring {
	return &wiring{d: impl{}, a: &unit{}, b: &unit{}}
}

func nonWiringStruct() *mixed {
	return &mixed{a: nil}
}

func byAssignment() *wiring {
	w := new(wiring) // want "wiring is constructed with d left nil"
	w.a = &unit{}
	w.b = &unit{}

	return w
}

func fullyAssigned() *wiring {
	w := &wiring{}
	w.d = impl{}
	w.a = &unit{}
	w.b = &unit{}

	return w
}

func passedThroughUnwritten() *wiring {
	w := new(wiring)

	return w
}

func byVarDecl() *wiring {
	var w = new(wiring) // want "wiring is constructed with d, b left nil"
	w.a = &unit{}

	return w
}
