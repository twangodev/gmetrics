package people

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"math"
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

type sectionLayout struct {
	avatarSize  int
	columnStep  int
	columns     int
	peopleCount int
	overflow    int
}

func (l sectionLayout) slots() int {
	n := l.peopleCount
	if l.overflow > 0 {
		n++
	}
	return n
}

func (*Plugin) Render(env *plugin.Env, raw any) (plugin.Fragment, error) {
	data, ok := raw.(Data)
	if !ok {
		return plugin.Fragment{}, fmt.Errorf("people: render: want Data, got %T", raw)
	}
	if err := validateLayout(data.Size, data.MaxOverlap); err != nil {
		return plugin.Fragment{}, err
	}

	headerFace, err := render.Face(14, canvas.FontBold)
	if err != nil {
		return plugin.Fragment{}, fmt.Errorf("people: load header face: %w", err)
	}

	var buf bytes.Buffer
	y := 0
	for _, section := range data.Sections {
		layout := layoutSection(section, data.Size, data.MaxOverlap)
		if err := writeSection(&buf, section, y, layout, headerFace); err != nil {
			return plugin.Fragment{}, err
		}
		y += layout.height()
	}

	return plugin.Fragment{
		Body:   buf.String(),
		Width:  fragmentWidth,
		Height: y,
	}, nil
}

func layoutSection(section Section, size int, maxOverlap float64) sectionLayout {
	step := max(1, int(math.Ceil(float64(size)*(1-maxOverlap))))
	if maxOverlap == 0 {
		step = size + rowGap
	}
	return sectionLayout{
		avatarSize:  size,
		columnStep:  step,
		columns:     1 + (fragmentWidth-size)/step,
		peopleCount: len(section.People),
		overflow:    max(0, section.Total-len(section.People)),
	}
}

func (l sectionLayout) height() int {
	rows := (l.slots() + l.columns - 1) / l.columns
	return headerHeight + rows*(l.avatarSize+rowGap) + sectionPadBot
}

func (l sectionLayout) position(i int) (int, int) {
	return (i % l.columns) * l.columnStep, headerHeight + (i/l.columns)*(l.avatarSize+rowGap)
}

func writeSection(buf *bytes.Buffer, s Section, y int, layout sectionLayout, headerFace *canvas.FontFace) error {
	fmt.Fprintf(buf, `<g class="people-section" data-type="%s" transform="translate(0,%d)">`,
		xmlEscape(s.Type), y)

	// s.Total, not len(s.People): a truncated list still labels the full count.
	header := fmt.Sprintf("%d %s", s.Total, sectionLabel(s.Type, s.Total))
	render.EmitOcticon(buf, 0, 6, 16, "people", "#959da5")
	render.EmitTextPath(buf, 22, 18, header, headerFace)

	for i, p := range s.People {
		x, cy := layout.position(i)
		writeAvatar(buf, p, x, cy, layout.avatarSize)
	}
	if layout.overflow > 0 {
		x, cy := layout.position(layout.peopleCount)
		if err := writeOverflow(buf, layout.overflow, x, cy, layout.avatarSize); err != nil {
			return fmt.Errorf("people: render overflow: %w", err)
		}
	}

	fmt.Fprint(buf, `</g>`)
	return nil
}

func writeAvatar(buf *bytes.Buffer, p Person, x, y, size int) {
	accountType := "user"
	if p.IsOrganization {
		accountType = "organization"
	}
	if p.AvatarB64 != "" {
		// Per-avatar <clipPath> with a unique id: renderers like resvg/librsvg
		// don't support the inline `clip-path: circle()` shorthand.
		clipID := fmt.Sprintf("avatar-clip-%s-%d-%d", p.Login, x, y)
		fmt.Fprintf(buf, `<defs><clipPath id="%s">`, clipID)
		writeAvatarShape(buf, p.IsOrganization, x, y, size, "")
		fmt.Fprintf(buf,
			`</clipPath></defs><image data-account-type="%s" x="%d" y="%d" width="%d" height="%d" href="%s" clip-path="url(#%s)" preserveAspectRatio="xMidYMid slice"><title>%s</title></image>`,
			accountType, x, y, size, size, xmlEscapeAttr(p.AvatarB64), clipID, xmlEscape(p.Login),
		)
	} else {
		fmt.Fprintf(buf, `<g data-account-type="%s"><title>%s</title>`, accountType, xmlEscape(p.Login))
		writeAvatarShape(buf, p.IsOrganization, x, y, size, ` fill="#d0d7de"`)
		fmt.Fprint(buf, `</g>`)
	}
	writeAvatarBorder(buf, p.IsOrganization, x, y, size)
}

func writeAvatarBorder(buf *bytes.Buffer, organization bool, x, y, size int) {
	if size < 3 {
		return
	}
	writeAvatarShape(buf, organization, x+1, y+1, size-2,
		` class="people-avatar-border" fill="none" stroke="#ffffff" stroke-width="2"`)
}

func writeAvatarShape(buf *bytes.Buffer, organization bool, x, y, size int, attrs string) {
	if organization {
		cornerRadius := size / 6
		if cornerRadius < 2 {
			cornerRadius = 2
		}
		fmt.Fprintf(buf,
			`<rect x="%d" y="%d" width="%d" height="%d" rx="%d" ry="%d"%s/>`,
			x, y, size, size, cornerRadius, cornerRadius, attrs,
		)
		return
	}
	fmt.Fprintf(buf, `<circle cx="%g" cy="%g" r="%g"%s/>`, float64(x)+float64(size)/2, float64(y)+float64(size)/2, float64(size)/2, attrs)
}

func writeOverflow(buf *bytes.Buffer, hidden, x, y, size int) error {
	label := formatOverflow(hidden)
	fontSize := math.Min(10, math.Max(4, float64(size)*0.45))
	var face *canvas.FontFace
	var err error
	for fontSize >= 4 {
		face, err = render.Face(fontSize, canvas.FontBold)
		if err != nil {
			return err
		}
		if render.TextWidth(face, label) <= float64(size-4) {
			break
		}
		fontSize -= 0.5
	}

	fmt.Fprintf(buf,
		`<g class="people-overflow" data-overflow="%d"><title>%d more</title>`,
		hidden, hidden,
	)
	writeAvatarShape(buf, false, x, y, size, ` fill="#d0d7de"`)
	writeAvatarBorder(buf, false, x, y, size)
	textWidth := render.TextWidth(face, label)
	textX := x + int((float64(size)-textWidth)/2+0.5)
	baselineY := y + size/2 + int(fontSize*0.35+0.5)
	render.EmitTextPathColor(buf, textX, baselineY, label, face, "#57606a")
	fmt.Fprint(buf, `</g>`)
	return nil
}

func formatOverflow(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("+%dM", n/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("+%dK", n/1_000)
	default:
		return fmt.Sprintf("+%d", n)
	}
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
