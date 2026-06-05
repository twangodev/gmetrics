package languages

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

const (
	fragmentWidth   = 440
	barHeight       = 8
	h2BaselineY     = 18
	h3BaselineY     = 38
	barY            = 52
	legendStartY    = 68
	legendRowHeight = 22
	legendBottomPad = 16

	detailPercentage = "percentage"
)

func renderFragment(_ *plugin.Env, data Data) (plugin.Fragment, error) {
	h2Face, err := render.Face(16, canvas.FontRegular)
	if err != nil {
		return plugin.Fragment{}, fmt.Errorf("languages: load h2 face: %w", err)
	}
	h3Face, err := render.Face(14, canvas.FontRegular)
	if err != nil {
		return plugin.Fragment{}, fmt.Errorf("languages: load h3 face: %w", err)
	}
	legendFace, err := render.Face(12, canvas.FontRegular)
	if err != nil {
		return plugin.Fragment{}, fmt.Errorf("languages: load legend face: %w", err)
	}
	captionFace, err := render.Face(11, canvas.FontRegular)
	if err != nil {
		return plugin.Fragment{}, fmt.Errorf("languages: load caption face: %w", err)
	}

	langs := data.Langs
	legendRows := (len(langs) + 1) / 2
	height := legendStartY + legendRows*legendRowHeight + legendBottomPad

	var buf bytes.Buffer

	h2Text := fmt.Sprintf("%d Languages", len(langs))
	render.EmitOcticon(&buf, 0, h2BaselineY-12, 16, "code", "#959da5")
	render.EmitTextPathClass(&buf, 22, h2BaselineY, h2Text, h2Face, "text-heading")

	render.EmitTextPath(&buf, 0, h3BaselineY, "Most used languages", h3Face)
	if data.Indepth && data.IndepthCommits > 0 {
		caption := fmt.Sprintf("from %s LoC across %s files in %s commits",
			humanizeCount(data.IndepthLines),
			humanizeCount(data.IndepthFiles),
			humanizeCount(data.IndepthCommits),
		)
		render.EmitTextPathRightAlignedClass(&buf, fragmentWidth, h3BaselineY, caption, captionFace, "text-muted")
	}

	fmt.Fprintf(&buf, `<g class="languages-bar" transform="translate(0,%d)">`, barY)

	if len(langs) == 0 {
		fmt.Fprintf(&buf,
			`<rect x="0" y="0" width="%d" height="%d" rx="3" fill="#d1d5da"/>`,
			fragmentWidth, barHeight,
		)
	} else {
		fmt.Fprintf(&buf, `<clipPath id="languages-bar-clip"><rect x="0" y="0" width="%d" height="%d" rx="3"/></clipPath>`,
			fragmentWidth, barHeight,
		)
		fmt.Fprintf(&buf, `<g clip-path="url(#languages-bar-clip)">`)
		// Round at segment boundaries, not widths, so rounding error never opens a gap.
		x := 0.0
		for i, l := range langs {
			segWidth := l.Percent * fragmentWidth
			x0 := int(math.Round(x))
			x1 := int(math.Round(x + segWidth))
			if i == len(langs)-1 {
				x1 = fragmentWidth
			}
			w := x1 - x0
			if w < 0 {
				w = 0
			}
			fmt.Fprintf(&buf,
				`<rect x="%d" y="0" width="%d" height="%d" fill="%s"><title>%s</title></rect>`,
				x0, w, barHeight, xmlEscapeAttr(l.Color), xmlEscape(percentSummary(l)),
			)
			x += segWidth
		}
		fmt.Fprintf(&buf, `</g>`)
	}
	fmt.Fprintf(&buf, `</g>`)

	colWidth := fragmentWidth / 2
	for i, l := range langs {
		col := i % 2
		row := i / 2
		x := col * colWidth
		y := legendStartY + row*legendRowHeight
		writeLegendRow(&buf, l, x, y, data.Details, legendFace)
	}

	return plugin.Fragment{
		Body:   buf.String(),
		Width:  fragmentWidth,
		Height: height,
	}, nil
}

func writeLegendRow(buf *bytes.Buffer, l Lang, x, y int, details []string, face *canvas.FontFace) {
	const circleR = 5
	cx := x + circleR + 1
	cy := y + legendRowHeight/2
	fmt.Fprintf(buf,
		`<circle cx="%d" cy="%d" r="%d" fill="%s"/>`,
		cx, cy, circleR, xmlEscapeAttr(l.Color),
	)

	textX := cx + circleR + 6
	textY := y + legendRowHeight/2 + 4
	render.EmitTextPath(buf, textX, textY, l.Name, face)

	if includesString(details, detailPercentage) {
		colRightEdge := x + (fragmentWidth / 2) - 8
		render.EmitTextPathRightAlignedClass(buf, colRightEdge, textY, formatPercent(l.Percent), face, "text-muted")
	}
}

func humanizeCount(n int) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(n)/1_000_000_000)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%dK", n/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func formatPercent(p float64) string {
	if math.IsNaN(p) || math.IsInf(p, 0) {
		return "0.0%"
	}
	return fmt.Sprintf("%.1f%%", p*100)
}

func percentSummary(l Lang) string {
	return fmt.Sprintf("%s (%s)", l.Name, formatPercent(l.Percent))
}

func includesString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func xmlEscapeAttr(s string) string {
	s = xmlEscape(s)
	// xml.EscapeText leaves double quotes intact; an attribute value must escape them.
	s = strings.ReplaceAll(s, `"`, `&quot;`)
	return s
}
