package excluded

var errNotFound = doSomethingErr()

func doWork() error {
	err := doSomething()
	if err != nil {
		return errNotFound
	}

	return nil
}

func doSomethingErr() error {
	return nil
}

func doSomething() error {
	return nil
}
