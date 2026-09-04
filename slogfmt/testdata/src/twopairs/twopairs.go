package twopairs

import "log/slog"

func f() {
	slog.Info("msg", "k1", 1, "k2", 2) // want "slog key-value arguments must each be on their own line"
}
