package plugin_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/twangodev/gmetrics/internal/plugin"
)

func TestErrorFragment_RendersIconAndMessage(t *testing.T) {
	frag := plugin.ErrorFragment("languages", errors.New("rate limited"))
	if !strings.Contains(frag.Body, "languages") || !strings.Contains(frag.Body, "rate limited") {
		t.Fatalf("expected plugin name and error in body; got %q", frag.Body)
	}
	if frag.Width == 0 || frag.Height == 0 {
		t.Fatalf("expected non-zero dimensions; got %dx%d", frag.Width, frag.Height)
	}
}

func TestErrorFragment_EscapesAngleBrackets(t *testing.T) {
	frag := plugin.ErrorFragment("test", errors.New("<script>x</script>"))
	if strings.Contains(frag.Body, "<script>") {
		t.Fatalf("error message must be XML-escaped; got %q", frag.Body)
	}
}
