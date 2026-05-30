package plugin

import (
	"bytes"
	"encoding/xml"
	"fmt"
)

func ErrorFragment(pluginName string, err error) Fragment {
	name := xmlEscape(pluginName)
	msg := xmlEscape(err.Error())
	body := fmt.Sprintf(`<g class="plugin-error" data-plugin="%s">
  <rect class="bg-error" x="0" y="0" width="440" height="40" rx="6"/>
  <text class="text-error" x="12" y="24" font-size="12">⚠ %s: %s</text>
</g>`, name, name, msg)
	return Fragment{Body: body, Width: 440, Height: 40}
}

func xmlEscape(s string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(s))
	return buf.String()
}
