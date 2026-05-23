package languages

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"math"
	"strings"

	"github.com/twangodev/gmetrics/internal/plugin"
)

// fragmentWidth is the working width every plugin draws against. It matches
// the value used by the engine when composing the outer SVG (440px content
// area inside a 480px frame, leaving 16+16+8 of margin/padding).
const fragmentWidth = 440

// barHeight is the height in pixels of the stacked-percentage bar.
const barHeight = 10

// barGap is the vertical pixels between the bar and the legend below it.
const barGap = 8

// headerHeight is the vertical space reserved for the "Languages" header
// label at the top of the fragment.
const headerHeight = 28

// legendRowHeight is the vertical pixels each legend row consumes.
const legendRowHeight = 22

// legendBottomPad is the extra padding at the bottom of the fragment so
// adjacent sections do not visually crowd each other.
const legendBottomPad = 16

// render lays out the languages fragment: a header, the horizontal stacked
// bar, and a 2-column legend with name + percentage per row. The returned
// Body is just the inner markup (no outer <svg>); the engine wraps it in a
// positioned <g> when composing the final card.
func render(_ *plugin.Env, data Data) (plugin.Fragment, error) {
	langs := data.Langs

	// Compute height: header + bar + gap + ceil(n/2) rows + bottom pad.
	legendRows := (len(langs) + 1) / 2 // ceil(n/2)
	height := headerHeight + barHeight + barGap + legendRows*legendRowHeight + legendBottomPad

	var buf bytes.Buffer

	// --- Header ---
	fmt.Fprintf(&buf,
		`<text x="0" y="18" font-size="14" font-weight="700" fill="var(--color-text)">Languages</text>`,
	)

	// --- Bar ---
	// The bar sits directly below the header band. We clip the row of
	// segments to a rounded mask so the corners look clean even when
	// individual segments meet exactly at the bar's edge.
	barY := headerHeight
	fmt.Fprintf(&buf, `<g class="languages-bar" transform="translate(0,%d)">`, barY)

	if len(langs) == 0 {
		// No data — draw a single neutral grey segment so the bar still
		// renders with a sensible placeholder rather than vanishing.
		fmt.Fprintf(&buf,
			`<rect x="0" y="0" width="%d" height="%d" rx="3" fill="#d1d5da"/>`,
			fragmentWidth, barHeight,
		)
	} else {
		// Stacked segments. We render the background as a single rect
		// with a rounded radius and then layer per-language rects on
		// top. Because each segment shares the same fill model, we get
		// the rounded ends "for free" via a clip-path on the parent.
		fmt.Fprintf(&buf, `<clipPath id="languages-bar-clip"><rect x="0" y="0" width="%d" height="%d" rx="3"/></clipPath>`,
			fragmentWidth, barHeight,
		)
		fmt.Fprintf(&buf, `<g clip-path="url(#languages-bar-clip)">`)
		x := 0.0
		// Allocate widths by accumulating exact float positions and
		// rounding to integers only at the rect boundaries. This avoids
		// per-segment rounding errors that would otherwise leave 1-2px
		// gaps at the right edge of the bar for long language lists.
		for i, l := range langs {
			segWidth := l.Percent * fragmentWidth
			x0 := int(math.Round(x))
			x1 := int(math.Round(x + segWidth))
			// The last segment always extends to fragmentWidth so we
			// never leave a sliver of background showing.
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

	// --- Legend (2-column) ---
	// Layout: each row holds up to 2 languages, side-by-side. The left
	// column starts at x=0; the right column at x = fragmentWidth/2.
	legendY := barY + barHeight + barGap
	colWidth := fragmentWidth / 2
	for i, l := range langs {
		col := i % 2
		row := i / 2
		x := col * colWidth
		y := legendY + row*legendRowHeight
		writeLegendRow(&buf, l, x, y, data.Details)
	}

	return plugin.Fragment{
		Body:   buf.String(),
		Width:  fragmentWidth,
		Height: height,
	}, nil
}

// writeLegendRow emits a single language row: colored circle + name +
// (optional) percentage. The cell is laid out left-to-right starting at
// (x, y) where y is the row's baseline-anchor (top of the row).
func writeLegendRow(buf *bytes.Buffer, l Lang, x, y int, details []string) {
	// Vertical center inside the row: the circle's center sits at y+11
	// (legendRowHeight / 2). The text baseline is offset for visual
	// alignment with the circle midline.
	const circleR = 5
	cx := x + circleR + 1
	cy := y + legendRowHeight/2
	fmt.Fprintf(buf,
		`<circle cx="%d" cy="%d" r="%d" fill="%s"/>`,
		cx, cy, circleR, xmlEscapeAttr(l.Color),
	)

	textX := cx + circleR + 6
	textY := y + legendRowHeight/2 + 4 // approximate visual baseline
	fmt.Fprintf(buf,
		`<text x="%d" y="%d" font-size="12" fill="var(--color-text)">%s</text>`,
		textX, textY, xmlEscape(l.Name),
	)

	if includesString(details, "percentage") {
		pctText := formatPercent(l.Percent)
		// Right-align the percentage at the right edge of the column.
		// We approximate width by drawing right-anchored via the
		// text-anchor="end" attribute and using x = (column right edge).
		colRightEdge := x + (fragmentWidth / 2) - 8
		fmt.Fprintf(buf,
			`<text x="%d" y="%d" font-size="12" text-anchor="end" fill="var(--color-muted)">%s</text>`,
			colRightEdge, textY, xmlEscape(pctText),
		)
	}
}

// formatPercent formats a 0..1 fraction as a "12.3%" style string.
func formatPercent(p float64) string {
	if math.IsNaN(p) || math.IsInf(p, 0) {
		return "0.0%"
	}
	return fmt.Sprintf("%.1f%%", p*100)
}

// percentSummary returns the title-attribute text shown on bar segment hover,
// e.g. "Go (45.2%)".
func percentSummary(l Lang) string {
	return fmt.Sprintf("%s (%s)", l.Name, formatPercent(l.Percent))
}

// includesString reports whether s appears in xs.
func includesString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// xmlEscape escapes content for use as text inside an SVG element.
func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// xmlEscapeAttr escapes content for use as an attribute value. SVG colors
// are short hex strings and so almost never need this, but using it
// consistently for every attribute keeps us safe if someone ever feeds an
// untrusted color string through.
func xmlEscapeAttr(s string) string {
	s = xmlEscape(s)
	s = strings.ReplaceAll(s, `"`, `&quot;`)
	return s
}
