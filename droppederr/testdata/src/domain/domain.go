package domain

type sentinel struct{ msg string }

func (e *sentinel) Error() string { return e.msg }

var ErrNotFound = &sentinel{msg: "not found"}

func WrapError(errs ...error) error {
	if len(errs) == 0 {
		return ErrNotFound
	}

	return errs[len(errs)-1]
}
