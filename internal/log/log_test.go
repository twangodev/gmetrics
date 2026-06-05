package log_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	gmlog "github.com/twangodev/gmetrics/internal/log"
)

func TestNewLogger_WritesKeyValuesInNonTTY(t *testing.T) {
	var buf bytes.Buffer
	logger := gmlog.NewLogger(gmlog.Options{Writer: &buf, NoColor: true, Level: slog.LevelInfo})
	logger.Info("hello", "k", "v")
	out := buf.String()
	if !strings.Contains(out, "hello") || !strings.Contains(out, "k=v") {
		t.Fatalf("expected 'hello' and 'k=v' in log output; got %q", out)
	}
}

func TestNewLogger_NilWriterDefaultsToStderr(t *testing.T) {
	l := gmlog.NewLogger(gmlog.Options{})
	if l == nil {
		t.Fatal("expected non-nil logger")
	}
}
