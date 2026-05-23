package people

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/twangodev/gmetrics/internal/plugin"
)

// fragmentWidth is the working width every plugin draws against. It matches
// the value used by the engine when composing the outer SVG (440px content
// area inside a 480px frame, leaving 16+16+8 of margin/padding).
const fragmentWidth = 440

// headerHeight is the vertical space reserved for the "<type> (<count>)"
// section header, in pixels.
const headerHeight = 28

// rowGap is the gap between avatar rows, in pixels. It is also used as the
// gap between columns; the per-cell stride is Size + rowGap.
const rowGap = 4

// sectionPadBot is extra padding at the bottom of each section so adjacent
// sections do not touch.
const sectionPadBot = 8

// Render lays out each Section as a header followed by a wrapping grid of
// avatar tiles and returns the resulting SVG fragment. The Fragment Body
// is the inner markup only (i.e. without an outer <svg>); the engine wraps
// it in a positioned <g>.
func (*Plugin) Render(env *plugin.Env, raw any) (plugin.Fragment, error) {
	data, ok := raw.(Data)
	if !ok {
		return plugin.Fragment{}, fmt.Errorf("people: render: want Data, got %T", raw)
	}
	if data.Size <= 0 {
		return plugin.Fragment{}, fmt.Errorf("people: render: invalid Size %d", data.Size)
	}

	cellSize := data.Size
	stride := cellSize + rowGap
	cols := fragmentWidth / stride
	if cols < 1 {
		cols = 1
	}

	var buf bytes.Buffer
	y := 0
	for _, section := range data.Sections {
		secH := sectionHeight(len(section.People), cellSize, cols)
		writeSection(&buf, section, y, cellSize, cols)
		y += secH
	}

	return plugin.Fragment{
		Body:   buf.String(),
		Width:  fragmentWidth,
		Height: y,
	}, nil
}

// sectionHeight returns the total pixel height a section with the given
// number of people will occupy. The math is: header band + ceil(n/cols) full
// stride rows + a small bottom pad. When the section is empty we still draw
// the header so the user can see the (0) count.
func sectionHeight(n, cellSize, cols int) int {
	rows := (n + cols - 1) / cols
	if rows == 0 {
		rows = 0
	}
	return headerHeight + rows*(cellSize+rowGap) + sectionPadBot
}

// writeSection emits one section's SVG into buf at the given y offset.
func writeSection(buf *bytes.Buffer, s Section, y, cellSize, cols int) {
	fmt.Fprintf(buf, `<g class="people-section" data-type="%s" transform="translate(0,%d)">`,
		xmlEscape(s.Type), y)

	// Header text: "<type> (<count>)". Bold, baseline 18px so the 28px band
	// has visual breathing room above and below.
	header := fmt.Sprintf("%s (%d)", titleCase(s.Type), len(s.People))
	fmt.Fprintf(buf,
		`<text x="0" y="18" font-size="14" font-weight="600" fill="var(--color-text)">%s</text>`,
		xmlEscape(header),
	)

	// Avatar grid. The header band is `headerHeight` tall; the first row of
	// avatars sits directly beneath it.
	for i, p := range s.People {
		col := i % cols
		row := i / cols
		x := col * (cellSize + rowGap)
		cy := headerHeight + row*(cellSize+rowGap)
		writeAvatar(buf, p, x, cy, cellSize)
	}

	fmt.Fprint(buf, `</g>`)
}

// writeAvatar emits a single avatar tile. When the embedded base64 URL is
// present we render an <image>; otherwise we draw a placeholder circle so
// the layout remains visible in test mode (env.HTTP == nil).
func writeAvatar(buf *bytes.Buffer, p Person, x, y, size int) {
	if p.AvatarB64 != "" {
		fmt.Fprintf(buf,
			`<image x="%d" y="%d" width="%d" height="%d" href="%s"><title>%s</title></image>`,
			x, y, size, size, xmlEscapeAttr(p.AvatarB64), xmlEscape(p.Login),
		)
		return
	}
	// Placeholder: a muted circle the same size as the avatar slot. We use
	// `currentColor` semantics via the theme variables so light/dark mode
	// both look acceptable.
	cx := x + size/2
	cy := y + size/2
	r := size / 2
	fmt.Fprintf(buf,
		`<circle cx="%d" cy="%d" r="%d" fill="var(--color-border)"><title>%s</title></circle>`,
		cx, cy, r, xmlEscape(p.Login),
	)
}

// titleCase upper-cases the first byte of s. We avoid x/text/cases for a
// single-character change and accept that "following" -> "Following" is
// always ASCII-safe in this scope (the only allowed values are
// "followers", "following").
func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// xmlEscape escapes content for use as text inside an SVG element.
func xmlEscape(s string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(s))
	return buf.String()
}

// xmlEscapeAttr escapes content for use as an attribute value. Data: URLs
// can contain '&' and '"' characters that must be escaped for the attribute
// to remain well-formed.
func xmlEscapeAttr(s string) string {
	// EscapeText already escapes &, <, >. For attribute values we also need
	// quotes; use Replace rather than xml's lower-level Encoder because the
	// helper is the same shape as xmlEscape.
	s = xmlEscape(s)
	s = strings.ReplaceAll(s, `"`, `&quot;`)
	return s
}
