package sqlutilpath

import (
	"dao"
	"domain"
)

var errNotFound = doSomethingErr()

func doWork() error {
	err := doSomething()
	if err != nil { // want "DROPPED: doWork drops the root error: err unused in return errNotFound"
		return errNotFound
	}

	return nil
}

func insertRaised() error {
	_, err := dao.Invoice.Insert(1)
	if err != nil {
		return domain.WrapError(err)
	}

	return nil
}

func doSomethingErr() error {
	return nil
}

func doSomething() error {
	return nil
}
