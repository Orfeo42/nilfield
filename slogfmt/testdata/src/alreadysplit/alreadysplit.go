package alreadysplit

import "log/slog"

func f() {
	slog.Info("msg",
		"k1", 1,
		"k2", 2,
	)
}
