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

const (
	fragmentWidth = 440

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

	graphColW   = 210.0 // graphColW*2 + graphColGap == fragmentWidth
	graphColGap = 20.0

	graphNameBarGap = 6.0
	graphBarPctGap  = 6.0

	defaultRowLimit = 5
	minBarW         = 20.0
)

// Fixed cycle kept legible on light and dark themes; the card CSS does not override these canvas colors.
var barPalette = []color.RGBA{
	{R: 0x57, G: 0xa6, B: 0xff, A: 0xff},
	{R: 0x5d, G: 0xe0, B: 0xa0, A: 0xff},
	{R: 0xff, G: 0xb1, B: 0x47, A: 0xff},
	{R: 0xff, G: 0x7b, B: 0x72, A: 0xff},
	{R: 0xbc, G: 0x8c, B: 0xff, A: 0xff},
	{R: 0x4d, G: 0xd0, B: 0xe1, A: 0xff},
}

// chartTextColor matches the CSS `text` default (#777777); canvas-rendered bars
// can't inherit the class, so the color is set explicitly.
var chartTextColor = color.RGBA{R: 0x77, G: 0x77, B: 0x77, A: 0xff}

func renderFragment(_ *plugin.Env, d Data) (plugin.Fragment, error) {
	proseBody, proseHeight, err := buildProseHeader(d)
	if err != nil {
		return plugin.Fragment{}, err
	}

	graphBody, graphHeight, err := buildGraphsSection(d)
	if err != nil {
		return plugin.Fragment{}, err
	}

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

type proseLine struct {
	icon string
	text string
}

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

	// `clock` octicon approximates upstream's stopwatch shape.
	render.EmitOcticon(&buf, 0, 0, iconSize, "clock", "#0366d6")
	render.EmitTextPathClass(&buf, iconSize+iconGutter, h2BaselineY, h2Label(d.Days), h2Face, "text-heading")

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

func computeGraphsHeight(d Data) int {
	pairs := pairedGraphSections(d.Sections)
	h := 0
	for _, pair := range pairs {
		h += sectionGap + int(pairCellHeight(d, pair))
	}
	return h
}

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

func pairedGraphSections(sections []string) [][]string {
	graphKeysInInputOrder := []string{}
	seen := map[string]bool{}
	for _, s := range sections {
		key, isGraph := splitSection(s)
		if !isGraph || seen[key] {
			continue
		}
		seen[key] = true
		graphKeysInInputOrder = append(graphKeysInInputOrder, key)
	}

	canonicalPairs := [][2]string{{"projects", "languages"}, {"editors", "os"}}
	ordered := []string{}
	placedInCanonicalPair := map[string]bool{}
	for _, pair := range canonicalPairs {
		a, b := pair[0], pair[1]
		if seen[a] && seen[b] {
			ordered = append(ordered, a, b)
			placedInCanonicalPair[a] = true
			placedInCanonicalPair[b] = true
		}
	}
	for _, k := range graphKeysInInputOrder {
		if placedInCanonicalPair[k] {
			continue
		}
		ordered = append(ordered, k)
	}

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

func categoryRowsKey(d Data, key string) int {
	return categoryRows(d, key+"-graphs")
}

func splitSection(s string) (key string, graph bool) {
	if strings.HasSuffix(s, "-graphs") {
		return strings.TrimSuffix(s, "-graphs"), true
	}
	return s, false
}

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

func categoryRows(d Data, section string) int {
	key, _ := splitSection(section)
	items := categoryFor(d, key)
	limit := d.Limit
	if limit <= 0 {
		limit = defaultRowLimit
	}
	if len(items) < limit {
		return len(items)
	}
	return limit
}

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

	drawLine(ctx, x0, y0, headerFace, captionFor(key))
	y := y0 + float64(graphHeader)

	const nameColMaxFrac = 0.60
	const valueColMax = 72.0
	nameColMax := nameColMaxFrac * cellW

	timeLabels := make([]string, len(items))
	nameColW, valueColW := 0.0, 0.0
	for i, it := range items {
		if w := render.TextWidth(rowFace, it.Name); w > nameColW {
			nameColW = w
		}
		timeLabels[i] = formatDuration(it.Seconds)
		if w := render.TextWidth(rowFace, timeLabels[i]); w > valueColW {
			valueColW = w
		}
	}
	if nameColW > nameColMax {
		nameColW = nameColMax
	}
	if valueColW > valueColMax {
		valueColW = valueColMax
	}

	nameX := x0
	valueRight := x0 + cellW
	valueX := valueRight - valueColW
	barX := nameX + nameColW + graphNameBarGap
	barW := valueX - graphBarPctGap - barX
	if barW < minBarW {
		barW = minBarW
	}

	maxSeconds := 0.0
	for _, it := range items {
		if it.Seconds > maxSeconds {
			maxSeconds = it.Seconds
		}
	}

	// drawLine's y is the text-box top, so center the row by offsetting half the face's visible height.
	rm := rowFace.Metrics()
	textVisualH := rm.Ascent + rm.Descent
	rowTextOffset := (graphRowH - textVisualH) / 2

	for i, it := range items {
		rowY := y + rowTextOffset
		drawLine(ctx, nameX, rowY, rowFace, render.TruncateToWidth(it.Name, rowFace, nameColW))

		frac := 0.0
		if maxSeconds > 0 {
			frac = it.Seconds / maxSeconds
		}
		barY := y + (graphRowH-barHeight)/2.0
		drawBarW(ctx, barX, barY, barW, frac, barColorFor(key, it.Name, i))

		timeLabel := timeLabels[i]
		rightAlignedX := valueRight - render.TextWidth(rowFace, timeLabel)
		drawLine(ctx, rightAlignedX, rowY, rowFace, timeLabel)
		y += graphRowH
	}
}

func formatDuration(seconds float64) string {
	totalMin := int(seconds/60 + 0.5)
	h, m := totalMin/60, totalMin%60
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	case h > 0:
		return fmt.Sprintf("%dh", h)
	default:
		return fmt.Sprintf("%dm", m)
	}
}

// y is the text-box top, not the baseline; face must have its Fill set.
func drawLine(ctx *canvas.Context, x, y float64, face *canvas.FontFace, s string) {
	if s == "" {
		return
	}
	rt := canvas.NewRichText(face)
	rt.WriteString(s)
	box := rt.ToText(fragmentWidth, 0, canvas.Left, canvas.Top, nil)
	ctx.DrawText(x, y, box)
}

// Paints only the filled fraction (0..1); no background track is drawn.
func drawBarW(ctx *canvas.Context, x, y, barW, frac float64, fill color.RGBA) {
	if frac <= 0 {
		return
	}
	if frac > 1 {
		frac = 1
	}
	// barHeight/2 radius gives pill ends; RoundedRectangle clamps it per side.
	ctx.Push()
	ctx.SetFillColor(fill)
	ctx.SetStrokeColor(color.RGBA{})
	ctx.DrawPath(x, y, canvas.RoundedRectangle(barW*frac, barHeight, barHeight/2))
	ctx.Pop()
}

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

func plural(v float64) string {
	if v == 1.0 {
		return ""
	}
	return "s"
}

func pluralInt(v int) string {
	if v == 1 {
		return ""
	}
	return "s"
}

func stringSet(xs []string) map[string]bool {
	out := make(map[string]bool, len(xs))
	for _, x := range xs {
		key, _ := splitSection(x)
		out[key] = true
	}
	return out
}

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

var (
	leadingSVG  = regexp.MustCompile(`^\s*<svg[^>]*>`)
	trailingSVG = regexp.MustCompile(`</svg>\s*$`)
)

// Strips the outer <svg> wrapper so the frame composer can nest the result.
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

// Renders text as outline paths instead of <text>, so GitHub's Camo proxy keeps it.
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
