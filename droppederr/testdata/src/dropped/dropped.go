package dropped

var errNotFound = doSomethingErr()

func doWork() error {
	err := doSomething()
	if err != nil { // want "DROPPED: doWork drops the root error: err unused in return errNotFound"
		return errNotFound
	}

	return nil
}

func doWorkUsed() error {
	err := doSomething()
	if err != nil {
		return err
	}

	return nil
}

func doSomethingErr() error {
	return nil
}

func doSomething() error {
	return nil
}
