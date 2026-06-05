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

// fragmentWidth must match the engine's content area inside the outer SVG.
const fragmentWidth = 440

const headerHeight = 28
const rowGap = 4
const sectionPadBot = 8

func (*Plugin) Render(env *plugin.Env, raw any) (plugin.Fragment, error) {
	data, ok := raw.(Data)
	if !ok {
		return plugin.Fragment{}, fmt.Errorf("people: render: want Data, got %T", raw)
	}
	if data.Size <= 0 {
		return plugin.Fragment{}, fmt.Errorf("people: render: invalid Size %d", data.Size)
	}

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

func sectionHeight(n, cellSize, cols int) int {
	rows := (n + cols - 1) / cols
	return headerHeight + rows*(cellSize+rowGap) + sectionPadBot
}

func writeSection(buf *bytes.Buffer, s Section, y, cellSize, cols int, headerFace *canvas.FontFace) {
	fmt.Fprintf(buf, `<g class="people-section" data-type="%s" transform="translate(0,%d)">`,
		xmlEscape(s.Type), y)

	// s.Total, not len(s.People): a truncated list still labels the full count.
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

func writeAvatar(buf *bytes.Buffer, p Person, x, y, size int) {
	cx := x + size/2
	cy := y + size/2
	r := size / 2
	if p.AvatarB64 != "" {
		// Per-avatar <clipPath> with a unique id: renderers like resvg/librsvg
		// don't support the inline `clip-path: circle()` shorthand.
		clipID := fmt.Sprintf("avatar-clip-%s-%d-%d", p.Login, x, y)
		fmt.Fprintf(buf,
			`<defs><clipPath id="%s"><circle cx="%d" cy="%d" r="%d"/></clipPath></defs><image x="%d" y="%d" width="%d" height="%d" href="%s" clip-path="url(#%s)" preserveAspectRatio="xMidYMid slice"><title>%s</title></image>`,
			clipID, cx, cy, r, x, y, size, size, xmlEscapeAttr(p.AvatarB64), clipID, xmlEscape(p.Login),
		)
		return
	}
	fmt.Fprintf(buf,
		`<circle cx="%d" cy="%d" r="%d" fill="#d0d7de"><title>%s</title></circle>`,
		cx, cy, r, xmlEscape(p.Login),
	)
}

func sectionLabel(t string, total int) string {
	switch t {
	case "followers":
		if total == 1 {
			return "follower"
		}
		return "followers"
	case "following":
		return "followed" // upstream classic-template label, not "following"
	default:
		return t
	}
}

func xmlEscape(s string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(s))
	return buf.String()
}

func xmlEscapeAttr(s string) string {
	// EscapeText handles &<>; attribute values additionally need quotes escaped.
	s = xmlEscape(s)
	s = strings.ReplaceAll(s, `"`, `&quot;`)
	return s
}
