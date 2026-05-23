package base

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tdewolff/canvas"
	"github.com/twangodev/gmetrics/internal/plugin"
	"github.com/twangodev/gmetrics/internal/render"
)

// Section heights in px. The frame composer runs each plugin fragment at
// roughly the inner card width (480-2*16 = 448), but the spec calls the
// base plugin a 440px-wide content area; the remaining 8px buffer keeps
// long strings from butting against the rounded corner of the frame.
const (
	sectionWidth = 440
	sectionGap   = 8

	headerHeight       = 84
	activityHeight     = 84
	communityHeight    = 84
	repositoriesHeight = 84
	metadataHeight     = 24
)

// Render lays out every enabled section in data.Sections and returns a
// single SVG fragment ready for the frame composer. Each text string is
// converted to glyph paths so the output is portable through GitHub's Camo
// image proxy (which strips foreignObject and external font references).
func Render(_ *plugin.Env, data Data) (plugin.Fragment, error) {
	reg14, err := render.Face(14, canvas.FontRegular)
	if err != nil {
		return plugin.Fragment{}, fmt.Errorf("base: load regular face: %w", err)
	}
	bold16, err := render.Face(16, canvas.FontBold)
	if err != nil {
		return plugin.Fragment{}, fmt.Errorf("base: load bold face: %w", err)
	}
	muted10, err := render.Face(10, canvas.FontRegular)
	if err != nil {
		return plugin.Fragment{}, fmt.Errorf("base: load small face: %w", err)
	}

	var body strings.Builder
	y := 0
	first := true
	for _, sec := range data.Sections {
		var h int
		var ok bool
		if !first {
			y += sectionGap
		}
		switch sec {
		case "header":
			h, ok = headerHeight, true
			renderHeader(&body, y, data, bold16, reg14)
		case "activity":
			h, ok = activityHeight, true
			renderStats(&body, y, "Activity", []statRow{
				{"Commits", data.Activity.Commits},
				{"Pull requests opened", data.Activity.PRsOpened},
				{"Pull requests reviewed", data.Activity.PRsReviewed},
				{"Issues opened", data.Activity.IssuesOpened},
			}, bold16, reg14)
		case "community":
			h, ok = communityHeight, true
			renderStats(&body, y, "Community", []statRow{
				{"Organizations", data.Community.Orgs},
				{"Following", data.Community.Following},
				{"Sponsoring", data.Community.Sponsors},
				{"Starred", data.Community.Stars},
			}, bold16, reg14)
		case "repositories":
			h, ok = repositoriesHeight, true
			renderStats(&body, y, "Repositories", []statRow{
				{"Repositories", data.Repositories.Count},
				{"Forks", data.Repositories.Forks},
				{"Watchers", data.Repositories.Watchers},
				{"Stargazers", data.Repositories.Stargazers},
			}, bold16, reg14)
		case "metadata":
			h, ok = metadataHeight, true
			renderMetadata(&body, y, data, muted10)
		}
		if !ok {
			continue
		}
		y += h
		first = false
	}

	height := y
	if height == 0 {
		height = 1 // emit a non-zero box even when every section was skipped
	}

	fragment := fmt.Sprintf(`<g class="plugin-base">%s</g>`, body.String())
	return plugin.Fragment{
		Body:   fragment,
		Width:  sectionWidth,
		Height: height,
	}, nil
}

// statRow is one "Label: count" line in an activity/community/repositories
// section. count is rendered with thousands separators via formatCount.
type statRow struct {
	label string
	count int
}

// renderHeader draws the avatar circle, the user's display name, the
// @login, and (when enabled) the "Available for hire" badge.
func renderHeader(w *strings.Builder, y0 int, d Data, nameFace, lineFace *canvas.FontFace) {
	// Avatar: 64x64 circle placeholder. The real avatar URL fetch happens
	// in a follow-up; v1 emits a neutral filled circle that matches the
	// upstream layout slot.
	cx := 32
	cy := y0 + 32
	fmt.Fprintf(w, `<circle cx="%d" cy="%d" r="32" fill="var(--color-border)"/>`, cx, cy)

	name := d.User.Name
	if name == "" {
		name = d.User.Login
	}
	if name == "" {
		name = "(unknown)"
	}

	// Name line ~18 px down from top so cap height aligns with avatar's top
	// quarter; the @login sits one line below.
	emitTextLine(w, 80, y0+24, name, nameFace)
	login := d.User.Login
	if login != "" {
		emitTextLine(w, 80, y0+46, "@"+login, lineFace)
	}

	if d.Hireable {
		// Badge bar: a small rect with "Available for hire" inside.
		bx := 80
		by := y0 + 60
		bw := 150
		bh := 18
		fmt.Fprintf(w, `<rect x="%d" y="%d" width="%d" height="%d" rx="4" class="bg-error" fill-opacity="0.3"/>`, bx, by, bw, bh)
		emitTextLine(w, bx+8, by+14, "Available for hire", lineFace)
	}
}

// renderStats draws a labelled section heading and up to four "stat: count"
// rows underneath. The vertical layout is fixed; long labels are not
// wrapped (v1 keeps the layout deterministic).
func renderStats(w *strings.Builder, y0 int, title string, rows []statRow, titleFace, rowFace *canvas.FontFace) {
	emitTextLine(w, 0, y0+18, title, titleFace)
	for i, r := range rows {
		// 4 rows starting at y0+36, spaced 14px apart.
		ry := y0 + 36 + i*14
		emitTextLine(w, 16, ry, r.label, rowFace)
		count := formatCount(r.count)
		// right-align the number at x = sectionWidth - 8.
		emitTextRightAligned(w, sectionWidth-8, ry, count, rowFace)
	}
}

// renderMetadata writes the single muted footer line.
func renderMetadata(w *strings.Builder, y0 int, d Data, face *canvas.FontFace) {
	msg := "Generated at " + d.Metadata.GeneratedAt
	emitTextLineClass(w, 0, y0+14, msg, face, "text-muted")
}

// emitTextLine draws a single line of text at (x, baselineY). The string is
// converted to glyph paths via the canvas font face; the resulting path is
// flipped vertically so the SVG coordinate system (Y down) matches the
// canvas one (Y up at the baseline).
func emitTextLine(w *strings.Builder, x, baselineY int, s string, face *canvas.FontFace) {
	emitTextLineClass(w, x, baselineY, s, face, "")
}

// emitTextLineClass is the class-aware variant of emitTextLine.
func emitTextLineClass(w *strings.Builder, x, baselineY int, s string, face *canvas.FontFace, class string) {
	if s == "" {
		return
	}
	d := pathDataFor(face, s)
	if d == "" {
		return
	}
	classAttr := ""
	if class != "" {
		classAttr = fmt.Sprintf(` class="%s"`, class)
	}
	// scale(1,-1) flips the Y axis: the canvas font produces an upward path
	// from the baseline; we want it growing downward in SVG.
	fmt.Fprintf(w, `<path%s transform="translate(%d,%d) scale(1,-1)" d="%s"/>`, classAttr, x, baselineY, d)
}

// emitTextRightAligned draws a line so its right edge sits at rightX.
func emitTextRightAligned(w *strings.Builder, rightX, baselineY int, s string, face *canvas.FontFace) {
	if s == "" {
		return
	}
	width := face.TextWidth(s)
	x := rightX - int(width+0.5)
	emitTextLine(w, x, baselineY, s, face)
}

// pathDataFor converts a string to SVG path data using the supplied face.
// Returns the empty string when the font produces no glyph paths (e.g.
// the input is whitespace only); callers fall through gracefully.
func pathDataFor(face *canvas.FontFace, s string) string {
	p, _, err := face.ToPath(s)
	if err != nil || p == nil || p.Empty() {
		return ""
	}
	return p.ToSVG()
}

// formatCount renders an int with thousands separators (", ").
func formatCount(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	s := strconv.Itoa(n)
	// insert commas from the right
	var b strings.Builder
	for i, r := range s {
		if i != 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}
