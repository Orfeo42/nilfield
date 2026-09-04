package sql_utility

func WrapQueryError(cause error) error { return cause }

func IsDuplicateKey(cause error) bool { return cause != nil }
