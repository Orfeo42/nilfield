package modulescope_dep

type Inner struct {
	N int
}

type Dep struct {
	Field *Inner
}
