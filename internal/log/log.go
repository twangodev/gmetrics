package log

import (
	"io"
	"log/slog"
	"os"

	"github.com/lmittmann/tint"
)

type Options struct {
	Writer  io.Writer
	Level   slog.Level
	NoColor bool
}

func NewLogger(opts Options) *slog.Logger {
	w := opts.Writer
	if w == nil {
		w = os.Stderr
	}
	handler := tint.NewHandler(w, &tint.Options{
		Level:   opts.Level,
		NoColor: opts.NoColor,
	})
	return slog.New(handler)
}
