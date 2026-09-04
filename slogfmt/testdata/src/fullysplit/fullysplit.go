package fullysplit

import "log/slog"

func f() {
	slog.Info(
		"msg",
		slog.String("k1", "v1"),
		slog.Int("k2", 2),
	)
}
