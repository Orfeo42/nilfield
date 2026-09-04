package sqlclass

import (
	"dao"
	"domain"
	"sql_utility"
)

type ext interface {
	QueryxContext(query string) (int, error)
}

type repository struct {
	ext ext
}

func (r *repository) findRaised() error {
	_, err := r.ext.QueryxContext("select 1")
	if err != nil { // want "SQLCLASS: findRaised drops the root error: database error raised via domain\\.WrapError, use sql_utility\\.WrapQueryError"
		return domain.WrapError(err)
	}

	return nil
}

func insertRaised() error {
	_, err := dao.Invoice.Insert(1)
	if err != nil { // want "SQLCLASS: insertRaised drops the root error: database error raised via domain\\.WrapError, use sql_utility\\.WrapQueryError"
		return domain.WrapError(err)
	}

	return nil
}

func insertWrapped() error {
	_, err := dao.Invoice.Insert(1)
	if err != nil {
		return sql_utility.WrapQueryError(err)
	}

	return nil
}

func insertGuarded() error {
	_, err := dao.Invoice.Insert(1)
	if err != nil {
		if sql_utility.IsDuplicateKey(err) {
			return domain.WrapError(err)
		}

		return err
	}

	return nil
}

func insertNestedInFuncLit() error {
	err := someWrapper(func() error {
		_, err := dao.Invoice.Insert(1)

		return err
	})
	if err != nil {
		return domain.WrapError(err)
	}

	return nil
}

func someWrapper(fn func() error) error {
	return fn()
}
