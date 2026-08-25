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

// Keep the people grid compact while preserving recognizable avatars. At the
// 440px content width, an 18px cell fits 20 columns and 40 total slots.
const maxPeopleRows = 2
const minAvatarSize = 18

type sectionLayout struct {
	cellSize      int
	cols          int
	visiblePeople int
	overflow      int
}

func (l sectionLayout) slots() int {
	n := l.visiblePeople
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
	if data.Size <= 0 {
		return plugin.Fragment{}, fmt.Errorf("people: render: invalid Size %d", data.Size)
	}

	headerFace, err := render.Face(14, canvas.FontBold)
	if err != nil {
		return plugin.Fragment{}, fmt.Errorf("people: load header face: %w", err)
	}

	var buf bytes.Buffer
	y := 0
	for _, section := range data.Sections {
		layout := layoutSection(section, data.Size)
		secH := sectionHeight(layout.slots(), layout.cellSize, layout.cols)
		if err := writeSection(&buf, section, y, layout, headerFace); err != nil {
			return plugin.Fragment{}, err
		}
		y += secH
	}

	return plugin.Fragment{
		Body:   buf.String(),
		Width:  fragmentWidth,
		Height: y,
	}, nil
}

func layoutSection(section Section, requestedSize int) sectionLayout {
	shrinkFloor := minAvatarSize
	if requestedSize < shrinkFloor {
		shrinkFloor = requestedSize
	}
	maxCols := columnsFor(shrinkFloor)
	maxSlots := maxPeopleRows * maxCols

	total := section.Total
	if total < len(section.People) {
		total = len(section.People)
	}
	visible := len(section.People)
	if visible > maxSlots {
		visible = maxSlots
	}
	overflow := total - visible
	if overflow > 0 && visible == maxSlots {
		visible--
		overflow = total - visible
	}

	slots := visible
	if overflow > 0 {
		slots++
	}
	cellSize := requestedSize
	if slots > 0 {
		requiredCols := (slots + maxPeopleRows - 1) / maxPeopleRows
		largestFit := (fragmentWidth - (requiredCols-1)*rowGap) / requiredCols
		if largestFit < cellSize {
			cellSize = largestFit
		}
		if cellSize < shrinkFloor {
			cellSize = shrinkFloor
		}
	}

	return sectionLayout{
		cellSize:      cellSize,
		cols:          columnsFor(cellSize),
		visiblePeople: visible,
		overflow:      overflow,
	}
}

func columnsFor(cellSize int) int {
	cols := (fragmentWidth + rowGap) / (cellSize + rowGap)
	if cols < 1 {
		return 1
	}
	return cols
}

func sectionHeight(n, cellSize, cols int) int {
	rows := (n + cols - 1) / cols
	return headerHeight + rows*(cellSize+rowGap) + sectionPadBot
}

func writeSection(buf *bytes.Buffer, s Section, y int, layout sectionLayout, headerFace *canvas.FontFace) error {
	fmt.Fprintf(buf, `<g class="people-section" data-type="%s" transform="translate(0,%d)">`,
		xmlEscape(s.Type), y)

	// s.Total, not len(s.People): a truncated list still labels the full count.
	header := fmt.Sprintf("%d %s", s.Total, sectionLabel(s.Type, s.Total))
	render.EmitOcticon(buf, 0, 6, 16, "people", "#959da5")
	render.EmitTextPath(buf, 22, 18, header, headerFace)

	for i, p := range s.People[:layout.visiblePeople] {
		x, cy := slotPosition(i, layout.cellSize, layout.cols)
		writeAvatar(buf, p, x, cy, layout.cellSize)
	}
	if layout.overflow > 0 {
		x, cy := slotPosition(layout.visiblePeople, layout.cellSize, layout.cols)
		if err := writeOverflow(buf, layout.overflow, x, cy, layout.cellSize); err != nil {
			return fmt.Errorf("people: render overflow: %w", err)
		}
	}

	fmt.Fprint(buf, `</g>`)
	return nil
}

func slotPosition(i, cellSize, cols int) (int, int) {
	col := i % cols
	row := i / cols
	return col * (cellSize + rowGap), headerHeight + row*(cellSize+rowGap)
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

	cx := x + size/2
	cy := y + size/2
	r := size / 2
	fmt.Fprintf(buf,
		`<g class="people-overflow" data-overflow="%d"><title>%d more</title><circle cx="%d" cy="%d" r="%d" fill="#d0d7de"/>`,
		hidden, hidden, cx, cy, r,
	)
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
