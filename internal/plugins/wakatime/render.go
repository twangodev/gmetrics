package wakatime

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"regexp"
	"strings"

	"github.com/tdewolff/canvas"
	svgrenderer "github.com/tdewolff/canvas/renderers/svg"
	"github.com/twangodev/gmetrics/internal/plugin"
	"github.com/twangodev/gmetrics/internal/render"
)

// Fragment dimensions and layout constants (in canvas units, i.e. pixels @ 1 DPMM).
const (
	fragmentWidth = 440
	headerHeight  = 24
	statsHeight   = 20
	graphHeader   = 18
	graphRowH     = 16
	plainRowH     = 18
	sectionGap    = 8

	barWidth     = 240.0
	barHeight    = 8.0
	barTrackOpacity = 0.15

	bodyFontPt   = 12.0
	headerFontPt = 14.0
)

// barPalette is a small fixed color cycle used for the graph bars. Distinct
// hues are kept simple so the rendered cards stay legible on both light and
// dark themes when previewed; the outer card CSS does not override these.
var barPalette = []color.RGBA{
	{R: 0x57, G: 0xa6, B: 0xff, A: 0xff}, // blue
	{R: 0x5d, G: 0xe0, B: 0xa0, A: 0xff}, // green
	{R: 0xff, G: 0xb1, B: 0x47, A: 0xff}, // amber
	{R: 0xff, G: 0x7b, B: 0x72, A: 0xff}, // red
	{R: 0xbc, G: 0x8c, B: 0xff, A: 0xff}, // purple
	{R: 0x4d, G: 0xd0, B: 0xe1, A: 0xff}, // cyan
}

var (
	mutedColor = color.RGBA{R: 0x57, G: 0x60, B: 0x6a, A: 0xff}
	textColor  = color.RGBA{R: 0x24, G: 0x29, B: 0x2f, A: 0xff}
)

// renderFragment composes the wakatime SVG fragment from previously-shaped
// Data. Caller-facing entry point used by Plugin.Render.
func renderFragment(_ *plugin.Env, d Data) (plugin.Fragment, error) {
	height := computeHeight(d)

	c, ctx := render.NewFragment(float64(fragmentWidth))
	// Top-left origin: y grows downward, which matches the layout math below.
	ctx.SetCoordSystem(canvas.CartesianIV)

	y := 0.0
	if err := drawHeader(ctx, d, &y); err != nil {
		return plugin.Fragment{}, err
	}
	if err := drawStats(ctx, d, &y); err != nil {
		return plugin.Fragment{}, err
	}

	for _, section := range d.Sections {
		key, isGraph := splitSection(section)
		items := categoryFor(d, key)
		if isGraph {
			if err := drawGraph(ctx, key, items, &y); err != nil {
				return plugin.Fragment{}, err
			}
		} else {
			if err := drawPlainSection(ctx, key, items, &y); err != nil {
				return plugin.Fragment{}, err
			}
		}
	}

	body, err := exportFragment(c)
	if err != nil {
		return plugin.Fragment{}, err
	}
	return plugin.Fragment{Body: body, Width: fragmentWidth, Height: height}, nil
}

// computeHeight sums per-section heights including inter-section gaps so the
// caller knows the exact fragment height before any drawing happens.
func computeHeight(d Data) int {
	h := headerHeight + sectionGap + statsHeight
	for _, section := range d.Sections {
		h += sectionGap
		_, isGraph := splitSection(section)
		if isGraph {
			rows := categoryRows(d, section)
			h += graphHeader + rows*graphRowH
		} else {
			h += plainRowH
		}
	}
	return h
}

// splitSection parses a section identifier like "languages-graphs" into its
// base key ("languages") and a flag indicating whether the bar-chart variant
// was requested.
func splitSection(s string) (key string, graph bool) {
	if strings.HasSuffix(s, "-graphs") {
		return strings.TrimSuffix(s, "-graphs"), true
	}
	return s, false
}

// categoryFor returns the slice of Items for the given key on Data.
func categoryFor(d Data, key string) []Item {
	switch key {
	case "projects":
		return d.Projects
	case "languages":
		return d.Languages
	case "editors":
		return d.Editors
	case "os":
		return d.OSes
	default:
		return nil
	}
}

// categoryRows returns the number of rows we will actually draw for the graph
// variant of the given section.
func categoryRows(d Data, section string) int {
	key, _ := splitSection(section)
	items := categoryFor(d, key)
	limit := d.Limit
	if limit <= 0 {
		limit = 5
	}
	if len(items) < limit {
		return len(items)
	}
	return limit
}

// drawHeader draws the bold "WakaTime · last N days" title row.
func drawHeader(ctx *canvas.Context, d Data, y *float64) error {
	face, err := render.Face(headerFontPt, canvas.FontBold)
	if err != nil {
		return err
	}
	face.Fill = canvas.Paint{Color: textColor}
	label := fmt.Sprintf("WakaTime · last %d days", d.Days)
	drawLine(ctx, 0, *y+headerFontPt, face, label)
	*y += headerHeight
	return nil
}

// drawStats draws the muted "Total / Daily avg" statistics row.
func drawStats(ctx *canvas.Context, d Data, y *float64) error {
	face, err := render.Face(bodyFontPt, canvas.FontRegular)
	if err != nil {
		return err
	}
	face.Fill = canvas.Paint{Color: mutedColor}
	label := fmt.Sprintf("Total: %.1f hours    Daily avg: %.1f hours", d.TotalHours, d.DailyAvgHours)
	*y += sectionGap
	drawLine(ctx, 0, *y+bodyFontPt, face, label)
	*y += statsHeight
	return nil
}

// drawPlainSection draws a one-line "Top: A (X%), B (Y%), ..." style row for
// the non-graph variant of a section.
func drawPlainSection(ctx *canvas.Context, key string, items []Item, y *float64) error {
	face, err := render.Face(bodyFontPt, canvas.FontRegular)
	if err != nil {
		return err
	}
	face.Fill = canvas.Paint{Color: textColor}
	*y += sectionGap

	prefix := captionFor(key)
	var label string
	if len(items) == 0 {
		label = prefix + ": no data"
	} else {
		parts := make([]string, 0, len(items))
		for _, it := range items {
			parts = append(parts, fmt.Sprintf("%s (%d%%)", it.Name, int(it.Percent*100+0.5)))
		}
		label = prefix + ": " + strings.Join(parts, ", ")
	}
	drawLine(ctx, 0, *y+bodyFontPt, face, label)
	*y += plainRowH
	return nil
}

// drawGraph draws a bar-chart section header followed by up to Limit rows
// (name | bar | percent).
func drawGraph(ctx *canvas.Context, key string, items []Item, y *float64) error {
	headerFace, err := render.Face(bodyFontPt, canvas.FontBold)
	if err != nil {
		return err
	}
	headerFace.Fill = canvas.Paint{Color: textColor}
	rowFace, err := render.Face(bodyFontPt, canvas.FontRegular)
	if err != nil {
		return err
	}
	rowFace.Fill = canvas.Paint{Color: textColor}

	*y += sectionGap
	drawLine(ctx, 0, *y+bodyFontPt, headerFace, captionFor(key))
	*y += graphHeader

	const nameColW = 110.0
	const barX = 120.0
	for i, it := range items {
		rowY := *y + graphRowH - 4
		drawLine(ctx, 0, rowY, rowFace, truncate(it.Name, 14))

		barY := *y + (graphRowH-barHeight)/2.0
		drawBar(ctx, barX, barY, it.Percent, barPalette[i%len(barPalette)])

		percent := fmt.Sprintf("%d%%", int(it.Percent*100+0.5))
		_ = nameColW
		drawLine(ctx, barX+barWidth+8, rowY, rowFace, percent)
		*y += graphRowH
	}
	return nil
}

// drawLine writes a single line of text starting at (x, baselineTopY). The
// face must have its Fill set before calling.
func drawLine(ctx *canvas.Context, x, y float64, face *canvas.FontFace, s string) {
	if s == "" {
		return
	}
	rt := canvas.NewRichText(face)
	rt.WriteString(s)
	box := rt.ToText(fragmentWidth, 0, canvas.Left, canvas.Top, nil)
	ctx.DrawText(x, y, box)
}

// drawBar paints both the track (faint background) and the filled portion of
// a horizontal bar at (x, y) with the given normalized fraction (0..1).
func drawBar(ctx *canvas.Context, x, y, frac float64, fill color.RGBA) {
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}

	// Track
	track := fill
	track.A = uint8(float64(track.A) * barTrackOpacity)
	ctx.Push()
	ctx.SetFillColor(track)
	ctx.SetStrokeColor(color.RGBA{})
	ctx.DrawPath(x, y, canvas.Rectangle(barWidth, barHeight))
	ctx.Pop()

	if frac == 0 {
		return
	}
	ctx.Push()
	ctx.SetFillColor(fill)
	ctx.SetStrokeColor(color.RGBA{})
	ctx.DrawPath(x, y, canvas.Rectangle(barWidth*frac, barHeight))
	ctx.Pop()
}

// captionFor returns the human label for a category key.
func captionFor(key string) string {
	switch key {
	case "projects":
		return "Projects"
	case "languages":
		return "Languages"
	case "editors":
		return "Editors"
	case "os":
		return "Operating systems"
	case "time":
		return "Time"
	default:
		return key
	}
}

// truncate trims a string to n runes with an ellipsis if it would overflow.
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}

// outerSVGTag matches the leading `<svg ...>` and trailing `</svg>` so we can
// strip them from the canvas SVG and leave only the inner drawing elements.
var (
	leadingSVG  = regexp.MustCompile(`^\s*<svg[^>]*>`)
	trailingSVG = regexp.MustCompile(`</svg>\s*$`)
)

// exportFragment serializes the canvas to SVG with text rendered as outline
// paths (no embedded font, no `<text>` elements that would depend on a font
// being present at view time) and strips the outer <svg> wrapper so the
// frame composer can position it inside the parent document.
func exportFragment(c *canvas.Canvas) (string, error) {
	w, h := c.Size()
	if h <= 0 {
		h = 1
	}
	var buf bytes.Buffer
	opts := svgrenderer.Options{
		EmbedFonts:  false,
		SubsetFonts: false,
		SizeUnits:   "",
	}
	r := svgrenderer.New(&buf, w, h, &opts)
	pr := &pathRenderer{inner: r}
	c.RenderTo(pr)
	if err := r.Close(); err != nil {
		return "", fmt.Errorf("wakatime: close svg: %w", err)
	}
	body := buf.String()
	body = leadingSVG.ReplaceAllString(body, "")
	body = trailingSVG.ReplaceAllString(body, "")
	return body, nil
}

// pathRenderer wraps an SVG renderer so that text is rendered as outline
// paths instead of `<text>` elements (mirroring the spec's "Outline: true"
// requirement for Camo-proof SVG output).
type pathRenderer struct {
	inner *svgrenderer.SVG
}

func (p *pathRenderer) Size() (float64, float64) { return p.inner.Size() }

func (p *pathRenderer) RenderPath(path *canvas.Path, style canvas.Style, m canvas.Matrix) {
	p.inner.RenderPath(path, style, m)
}

func (p *pathRenderer) RenderText(text *canvas.Text, m canvas.Matrix) {
	text.RenderAsPath(p.inner, m, canvas.DefaultResolution)
}

func (p *pathRenderer) RenderImage(img image.Image, m canvas.Matrix) {
	p.inner.RenderImage(img, m)
}
