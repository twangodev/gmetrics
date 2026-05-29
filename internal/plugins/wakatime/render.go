package wakatime

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"math"
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

	// Header / prose block sizing. The h2 sits on a 24px tall band; each
	// prose line below it is 20px tall. The block height is computed in
	// computeProseHeight so the canvas-drawn bar charts start at exactly
	// the right Y.
	h2BaselineY = 16
	h2BlockH    = 24
	proseLineH  = 20
	iconSize    = 16
	iconGutter  = 8

	graphHeader = 18
	graphRowH   = 16
	sectionGap  = 8
	barHeight   = 8.0

	bodyFontPt   = 12.0
	headerFontPt = 14.0

	// Two-column layout for the graph sections. Each cell is graphColW wide
	// and sits in a 2-column grid with graphColGap horizontal padding between
	// the two cells. (graphColW * 2 + graphColGap == fragmentWidth.)
	graphColW   = 210.0
	graphColGap = 20.0
	// Within a cell, the layout is: name | bar | percent. The name column
	// shrink-wraps to the widest label per cell (capped at 60% in
	// drawGraphCell). The percent text sits in a fixed-width slot on the
	// right; the bar fills whatever's left in between.
	graphNameBarGap  = 6.0
	graphBarPctGap   = 6.0
	graphPercentColW = 32.0
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

// chartTextColor matches the card's body text (#777777 — the EmitTextPath
// and CSS `text` default used by the prose summary and every other plugin).
// The bar charts are canvas-rendered, so they can't inherit the CSS class
// and must set this explicitly; without it the rows render in a near-black
// that nothing else on the card uses. Header rows reuse the same gray and
// rely on their bold, larger face for hierarchy (as the music card does).
var chartTextColor = color.RGBA{R: 0x77, G: 0x77, B: 0x77, A: 0xff}

// renderFragment composes the wakatime SVG fragment. The output stacks an
// upstream-style prose header (h2 title + per-category one-line summaries)
// on top of the existing canvas-drawn bar-chart sections. Caller-facing
// entry point used by Plugin.Render.
func renderFragment(_ *plugin.Env, d Data) (plugin.Fragment, error) {
	// 1. Build the prose header in raw SVG (text-as-path via the render
	//    helpers, matching the music / people plugins). The output is
	//    measured up front so the canvas section knows its Y offset.
	proseBody, proseHeight, err := buildProseHeader(d)
	if err != nil {
		return plugin.Fragment{}, err
	}

	// 2. Render the bar charts via the canvas pipeline. Only the
	//    "*-graphs" sections produce visible output here; the plain-text
	//    sections are already covered by the prose header.
	graphBody, graphHeight, err := buildGraphsSection(d)
	if err != nil {
		return plugin.Fragment{}, err
	}

	// 3. Compose: prose first, then a translated <g> wrapping the canvas
	//    output so its top-left origin lands at the right Y.
	var buf bytes.Buffer
	buf.WriteString(proseBody)
	if graphBody != "" {
		fmt.Fprintf(&buf, `<g transform="translate(0,%d)">%s</g>`, proseHeight, graphBody)
	}

	height := proseHeight + graphHeight
	if height < proseHeight {
		height = proseHeight
	}

	return plugin.Fragment{Body: buf.String(), Width: fragmentWidth, Height: height}, nil
}

// proseLine is one icon+text line in the prose summary. text is left empty
// when the source data is missing so the caller can skip that row.
type proseLine struct {
	icon string
	text string
}

// buildProseHeader emits the upstream-style h2 + per-category summary
// lines for the wakatime card. Returns the raw SVG body, the consumed
// vertical pixels, and any font-loading error.
func buildProseHeader(d Data) (string, int, error) {
	h2Face, err := render.Face(16, canvas.FontRegular)
	if err != nil {
		return "", 0, fmt.Errorf("wakatime: load h2 face: %w", err)
	}
	lineFace, err := render.Face(12, canvas.FontRegular)
	if err != nil {
		return "", 0, fmt.Errorf("wakatime: load prose-line face: %w", err)
	}

	var buf bytes.Buffer

	// h2: "WakaTime (over last <window>)" rendered in the heading colour.
	// Upstream uses a stopwatch-shaped octicon here; we approximate with
	// the `clock` octicon, which is already in our map.
	render.EmitOcticon(&buf, 0, 0, iconSize, "clock", "#0366d6")
	render.EmitTextPathClass(&buf, iconSize+iconGutter, h2BaselineY, h2Label(d.Days), h2Face, "text-heading")

	// Per-category prose lines, split into 2 columns to mirror the upstream
	// classic template (and the bar-chart grid below it). Left column gets
	// total-time / project / language; right gets daily-time / editor / os.
	sections := stringSet(d.Sections)
	textW := float64(graphColW) - float64(iconSize+iconGutter)

	left := []proseLine{}
	right := []proseLine{}
	if sections["time"] {
		hours := int(math.Round(d.TotalHours))
		left = append(left, proseLine{
			icon: "clock",
			text: fmt.Sprintf("%d coding hour%s recorded", hours, plural(d.TotalHours)),
		})
	}
	if sections["projects"] && len(d.Projects) > 0 {
		left = append(left, proseLine{
			icon: "repo",
			text: render.TruncateToWidth("Working on "+d.Projects[0].Name, lineFace, textW),
		})
	}
	if sections["languages"] && len(d.Languages) > 0 {
		left = append(left, proseLine{
			icon: "code",
			text: render.TruncateToWidth("Mostly coding in "+d.Languages[0].Name, lineFace, textW),
		})
	}
	if sections["time"] {
		daily := int(math.Round(d.DailyAvgHours))
		right = append(right, proseLine{
			icon: "pulse-triangles",
			text: fmt.Sprintf("~%d hour%s of coding per day", daily, pluralInt(daily)),
		})
	}
	if sections["editors"] && len(d.Editors) > 0 {
		right = append(right, proseLine{
			icon: "terminal",
			text: render.TruncateToWidth("Coding with "+d.Editors[0].Name, lineFace, textW),
		})
	}
	if sections["os"] && len(d.OSes) > 0 {
		right = append(right, proseLine{
			icon: "device-desktop",
			text: render.TruncateToWidth("Using "+d.OSes[0].Name, lineFace, textW),
		})
	}

	emitProseColumn := func(col []proseLine, xOffset int) {
		for i, ln := range col {
			y := h2BlockH + i*proseLineH
			render.EmitOcticon(&buf, xOffset, y+(proseLineH-iconSize)/2, iconSize, ln.icon, "#959da5")
			render.EmitTextPath(&buf, xOffset+iconSize+iconGutter, y+14, ln.text, lineFace)
		}
	}
	emitProseColumn(left, 0)
	emitProseColumn(right, int(graphColW+graphColGap))

	rows := len(left)
	if len(right) > rows {
		rows = len(right)
	}
	height := h2BlockH + rows*proseLineH + sectionGap
	return buf.String(), height, nil
}

// buildGraphsSection renders only the *-graphs entries from d.Sections
// using the canvas pipeline. Returns an empty body (and zero height) when
// no graph sections are requested.
//
// Layout (matches upstream lowlighter/metrics' .largeable flex pairing):
// graph sections are grouped into pairs and rendered side-by-side in a
// 2-column grid. The pair ordering follows upstream's template — the
// first column gets (projects, editors) and the second gets (languages,
// os) when all four are enabled.
func buildGraphsSection(d Data) (string, int, error) {
	pairs := pairedGraphSections(d.Sections)
	if len(pairs) == 0 {
		return "", 0, nil
	}
	height := computeGraphsHeight(d)
	if height <= 0 {
		return "", 0, nil
	}

	c, ctx := render.NewFragment(float64(fragmentWidth))
	ctx.SetCoordSystem(canvas.CartesianIV)

	y := 0.0
	for _, pair := range pairs {
		// Row height = max rows across the (1 or 2) cells in this pair.
		rowH := pairCellHeight(d, pair)
		y += sectionGap
		rowTop := y

		left := pair[0]
		leftItems := categoryFor(d, left)
		drawGraphCell(ctx, 0, rowTop, graphColW, left, leftItems)

		if len(pair) > 1 {
			right := pair[1]
			rightItems := categoryFor(d, right)
			drawGraphCell(ctx, graphColW+graphColGap, rowTop, graphColW, right, rightItems)
		}

		y += rowH
	}

	body, err := exportFragment(c)
	if err != nil {
		return "", 0, err
	}
	return body, height, nil
}

// computeGraphsHeight returns the vertical pixels the bar-chart block will
// occupy. Each row in the 2-column grid contributes a sectionGap + header
// + max(rowsLeft, rowsRight) * rowH.
func computeGraphsHeight(d Data) int {
	pairs := pairedGraphSections(d.Sections)
	h := 0
	for _, pair := range pairs {
		h += sectionGap + int(pairCellHeight(d, pair))
	}
	return h
}

// pairCellHeight returns the pixel height of a single row in the 2-column
// grid: graphHeader + maxRows*graphRowH across the (1 or 2) cells.
func pairCellHeight(d Data, pair []string) float64 {
	maxRows := 0
	for _, key := range pair {
		rows := categoryRowsKey(d, key)
		if rows > maxRows {
			maxRows = rows
		}
	}
	return float64(graphHeader + maxRows*graphRowH)
}

// pairedGraphSections groups the enabled `-graphs` sections into pairs for
// the 2-column grid. The upstream classic template arranges sections by
// the order they appear in d.Sections and pairs them as (col0, col1) within
// each row. Unpaired (odd) sections live alone in column 0.
//
// We additionally reorder so projects+languages and editors+os end up next
// to each other when all four are present (the upstream "pair order").
// The reordering is only applied when both halves of a canonical pair are
// present; otherwise we keep d.Sections' relative order so partial configs
// stay predictable.
func pairedGraphSections(sections []string) [][]string {
	// Collect graph keys in input order.
	keys := []string{}
	seen := map[string]bool{}
	for _, s := range sections {
		key, isGraph := splitSection(s)
		if !isGraph || seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}

	// Reorder so canonical pairs sit adjacent: (projects, languages) and
	// (editors, os). Any keys outside those pairs come last in their input
	// order.
	canonical := [][2]string{{"projects", "languages"}, {"editors", "os"}}
	ordered := []string{}
	used := map[string]bool{}
	for _, pair := range canonical {
		a, b := pair[0], pair[1]
		if seen[a] && seen[b] {
			ordered = append(ordered, a, b)
			used[a] = true
			used[b] = true
		}
	}
	for _, k := range keys {
		if used[k] {
			continue
		}
		ordered = append(ordered, k)
	}

	// Chunk into pairs of 2.
	out := [][]string{}
	for i := 0; i < len(ordered); i += 2 {
		if i+1 < len(ordered) {
			out = append(out, []string{ordered[i], ordered[i+1]})
		} else {
			out = append(out, []string{ordered[i]})
		}
	}
	return out
}

// categoryRowsKey is the bare-key variant of categoryRows.
func categoryRowsKey(d Data, key string) int {
	return categoryRows(d, key+"-graphs")
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

// drawGraphCell draws a bar-chart cell rooted at (x0, y0) with the given
// cell width. The header (14px bold) sits at the top; up to N rows of
// (name | bar | percent) follow underneath. key is the category identifier
// ("projects" / "languages" / "editors" / "os") and selects which color
// palette barColorFor uses.
func drawGraphCell(ctx *canvas.Context, x0, y0, cellW float64, key string, items []Item) {
	headerFace, err := render.Face(headerFontPt, canvas.FontBold)
	if err != nil {
		return
	}
	headerFace.Fill = canvas.Paint{Color: chartTextColor}
	rowFace, err := render.Face(bodyFontPt, canvas.FontRegular)
	if err != nil {
		return
	}
	rowFace.Fill = canvas.Paint{Color: chartTextColor}

	// Header text top is anchored to the cell top; drawLine treats its y
	// argument as the top of the text box (not the baseline), so passing y0
	// makes the header occupy y0..y0+graphHeader and the bar rows below it
	// start at y0+graphHeader without overlap.
	drawLine(ctx, x0, y0, headerFace, captionFor(key))
	y := y0 + float64(graphHeader)

	// Layout columns inside the cell. The name column shrink-wraps to the
	// widest label in this cell but caps at 60% of the cell width, so bars
	// always get at least the remaining ~40%. Labels longer than the cap
	// are pixel-truncated below.
	const nameColMaxFrac = 0.60
	nameColMax := nameColMaxFrac * cellW
	maxLabel := 0.0
	for _, it := range items {
		if w := rowFace.TextWidth(it.Name); w > maxLabel {
			maxLabel = w
		}
	}
	nameColW := maxLabel
	if nameColW > nameColMax {
		nameColW = nameColMax
	}
	nameX := x0
	pctX := x0 + cellW - graphPercentColW
	barX := nameX + nameColW + graphNameBarGap
	barW := pctX - graphBarPctGap - barX
	if barW < 20 {
		barW = 20
	}

	// Vertically center text and bar within the row. drawLine treats its y
	// argument as the top of the text box, so to land the text's vertical
	// center at graphRowH/2 we subtract half the face's visible height
	// (ascent + descent). The bar's top is already barH/2 above the same
	// center.
	rm := rowFace.Metrics()
	textVisualH := rm.Ascent + rm.Descent
	rowTextOffset := (graphRowH - textVisualH) / 2

	for i, it := range items {
		rowY := y + rowTextOffset
		drawLine(ctx, nameX, rowY, rowFace, render.TruncateToWidth(it.Name, rowFace, nameColW))

		barY := y + (graphRowH-barHeight)/2.0
		drawBarW(ctx, barX, barY, barW, it.Percent, barColorFor(key, it.Name, i))

		percent := fmt.Sprintf("%d%%", int(it.Percent*100+0.5))
		drawLine(ctx, pctX, rowY, rowFace, percent)
		y += graphRowH
	}
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

// drawBarW paints only the filled portion of a horizontal bar at (x, y)
// sized (barW × barHeight) with the given normalized fraction (0..1). The
// remaining (1 - frac) range is left as the card background — no faint
// track is drawn.
func drawBarW(ctx *canvas.Context, x, y, barW, frac float64, fill color.RGBA) {
	if frac <= 0 {
		return
	}
	if frac > 1 {
		frac = 1
	}
	ctx.Push()
	ctx.SetFillColor(fill)
	ctx.SetStrokeColor(color.RGBA{})
	ctx.DrawPath(x, y, canvas.Rectangle(barW*frac, barHeight))
	ctx.Pop()
}

// captionFor returns the human label for a category key.
func captionFor(key string) string {
	switch key {
	case "projects":
		return "Projects activity"
	case "languages":
		return "Language activity"
	case "editors":
		return "Code editors"
	case "os":
		return "Operating systems"
	case "time":
		return "Time"
	default:
		return key
	}
}

// h2Label returns the prose-style section title for the given lookback. The
// special cases mirror the upstream classic template ("week", "month",
// "6 months", "year"); anything else falls back to a generic "last N days".
func h2Label(days int) string {
	switch days {
	case 7:
		return "WakaTime (over last week)"
	case 30:
		return "WakaTime (over last month)"
	case 180:
		return "WakaTime (over last 6 months)"
	case 365:
		return "WakaTime (over last year)"
	default:
		return fmt.Sprintf("WakaTime (last %d days)", days)
	}
}

// plural returns "s" when v is anything but exactly 1, mirroring the
// upstream `s(...)` helper for English-language pluralisation of nouns
// that follow a float quantity.
func plural(v float64) string {
	if v == 1.0 {
		return ""
	}
	return "s"
}

// pluralInt is the integer-domain variant of plural.
func pluralInt(v int) string {
	if v == 1 {
		return ""
	}
	return "s"
}

// stringSet collapses a slice of section keys into a quick lookup map so
// the prose builder can ask "is X enabled?" without scanning the slice on
// each check. Both bare keys ("languages") and `-graphs` variants are
// canonicalised onto the bare key.
func stringSet(xs []string) map[string]bool {
	out := make(map[string]bool, len(xs))
	for _, x := range xs {
		key, _ := splitSection(x)
		out[key] = true
	}
	return out
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
