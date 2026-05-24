package people

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/tdewolff/canvas"
	"github.com/twangodev/gmetrics/internal/plugin"
	"github.com/twangodev/gmetrics/internal/render"
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

	// Build the header font face once and reuse across every section.
	headerFace, err := render.Face(14, canvas.FontBold)
	if err != nil {
		return plugin.Fragment{}, fmt.Errorf("people: load header face: %w", err)
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
		writeSection(&buf, section, y, cellSize, cols, headerFace)
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
func writeSection(buf *bytes.Buffer, s Section, y, cellSize, cols int, headerFace *canvas.FontFace) {
	fmt.Fprintf(buf, `<g class="people-section" data-type="%s" transform="translate(0,%d)">`,
		xmlEscape(s.Type), y)

	// "%d %s" uses s.Total (not the rendered avatar count) so a truncated
	// list still labels with the full upstream count.
	header := fmt.Sprintf("%d %s", s.Total, sectionLabel(s.Type, s.Total))
	render.EmitOcticon(buf, 0, 6, 16, "people", "#959da5")
	render.EmitTextPath(buf, 22, 18, header, headerFace)

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
		// Each avatar gets its own <clipPath> referencing a unique id so
		// SVG circle clipping works in renderers that don't support the
		// inline `clip-path: circle()` shorthand (resvg, librsvg, etc.).
		clipID := fmt.Sprintf("avatar-clip-%s-%d-%d", p.Login, x, y)
		cx := x + size/2
		cy := y + size/2
		r := size / 2
		fmt.Fprintf(buf,
			`<defs><clipPath id="%s"><circle cx="%d" cy="%d" r="%d"/></clipPath></defs><image x="%d" y="%d" width="%d" height="%d" href="%s" clip-path="url(#%s)" preserveAspectRatio="xMidYMid slice"><title>%s</title></image>`,
			clipID, cx, cy, r, x, y, size, size, xmlEscapeAttr(p.AvatarB64), clipID, xmlEscape(p.Login),
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
		`<circle cx="%d" cy="%d" r="%d" fill="#d0d7de"><title>%s</title></circle>`,
		cx, cy, r, xmlEscape(p.Login),
	)
}

// sectionLabel returns the lowercase, pluralized noun that follows the
// total count in a section header, matching the upstream lowlighter-metrics
// classic template. Unknown types fall back to the type name unchanged.
func sectionLabel(t string, total int) string {
	switch t {
	case "followers":
		if total == 1 {
			return "follower"
		}
		return "followers"
	case "following":
		// Upstream uses the past-tense form "followed" rather than the
		// gerund "following" for this section label.
		return "followed"
	default:
		return t
	}
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
