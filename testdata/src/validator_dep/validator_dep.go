package validator_dep

import "errors"

type Inner struct {
	N int
}

type Request struct {
	P *Inner
	Q *Inner
}

var ErrMissing = errors.New("missing")

func (r *Request) Validate() error {
	if r.P == nil {
		return ErrMissing
	}

	return nil
}
