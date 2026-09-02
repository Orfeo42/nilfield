package utility

func Assert(condition bool, message string) {
	if !condition {
		panic(message)
	}
}

func AssertWithCode(condition bool, code int, message string) {
	Assert(condition, message)
}
