package plugin

import (
	"bytes"
	"encoding/xml"
	"fmt"
)

// ErrorFragment produces a fragment that renders a graceful "this plugin failed"
// box. Used when a plugin's Fetch or Render returns an error and the engine is
// not in strict mode.
func ErrorFragment(pluginName string, err error) Fragment {
	// Use encoding/xml escaping for SVG-attribute safety.
	name := xmlEscape(pluginName)
	msg := xmlEscape(err.Error())
	body := fmt.Sprintf(`<g class="plugin-error" data-plugin="%s">
  <rect x="0" y="0" width="440" height="40" rx="6" fill="var(--color-error-bg)"/>
  <text x="12" y="24" fill="var(--color-error)" font-size="12">⚠ %s: %s</text>
</g>`, name, name, msg)
	return Fragment{Body: body, Width: 440, Height: 40}
}

// xmlEscape returns s with characters that are special in XML/SVG (such as
// '<', '>', '&', and quotes) replaced by their entity references. It is a
// thin wrapper around encoding/xml's EscapeText.
func xmlEscape(s string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(s))
	return buf.String()
}
