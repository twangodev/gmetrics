package render

import (
	"bytes"
	"strings"
	"testing"
)

func TestOcticonsMapHasGitCommit(t *testing.T) {
	d, ok := Octicons["git-commit"]
	if !ok {
		t.Fatal(`Octicons["git-commit"] missing`)
	}
	if d == "" {
		t.Fatal(`Octicons["git-commit"] is empty`)
	}
}

func TestEmitOcticonKnown(t *testing.T) {
	var buf bytes.Buffer
	EmitOcticon(&buf, 10, 20, 16, "git-commit", "#959da5")
	out := buf.String()

	wantSubstrings := []string{
		"<svg",
		`x="10"`,
		`width="16"`,
		`viewBox="0 0 16 16"`,
		`fill="#959da5"`,
		`d="M10.5 7.75`,
	}
	for _, s := range wantSubstrings {
		if !strings.Contains(out, s) {
			t.Errorf("output missing %q\nfull output: %s", s, out)
		}
	}
}

func TestEmitOcticonUnknown(t *testing.T) {
	var buf bytes.Buffer
	EmitOcticon(&buf, 0, 0, 16, "does-not-exist", "#000")
	if buf.Len() != 0 {
		t.Errorf("expected empty output for unknown icon, got %q", buf.String())
	}
}
