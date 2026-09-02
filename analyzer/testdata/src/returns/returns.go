package returns

import "errors"

type T struct{}

func okPair() (*T, error) { return &T{}, nil }

func failPair() (*T, error) { return nil, errors.New("x") }

func emptyPair() (*T, error) { return nil, nil } // want "nil value returned with a nil error" emptyPair:"may return nil result 0"

func sliceNil() ([]int, error) { return nil, nil }

func swallow() error {
	if err := work(); err != nil {
		return nil // want "err is discarded"
	}

	return nil
}

func propagate() error {
	if err := work(); err != nil {
		return err
	}

	return nil
}

func swallowElse() error {
	err := work()
	if err == nil {
		return nil
	} else {
		return nil // want "err is discarded"
	}
}

func work() error { return nil }
