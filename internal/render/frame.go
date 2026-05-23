package render

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/twangodev/gmetrics/internal/plugin"
)

// Options control the outer-SVG frame composer.
type Options struct {
	// Width is the total card width in pixels.
	Width int
	// Title is rendered as the SVG aria-label for screen readers.
	Title string
	// PadTop, PadGap, PadBot, PadSide control internal padding around the
	// stack of fragments. Zero means "use the default".
	PadTop, PadGap, PadBot, PadSide int
}

// Framer composes plugin fragments into a single outer SVG.
type Framer struct {
	opts Options
	tmpl *template.Template
}

// frameTmpl is a stdlib text/template producing the outer SVG. We use
// text/template rather than html/template because the fragment Body values
// are pre-sanitized SVG markup that must be emitted verbatim, not escaped.
const frameTmpl = `<svg xmlns="http://www.w3.org/2000/svg" width="{{.Width}}" height="{{.Height}}" viewBox="0 0 {{.Width}} {{.Height}}" role="img"{{if .Title}} aria-label="{{.Title}}"{{end}}>
<style>{{.CSS}}</style>
<rect class="frame" x="0.5" y="0.5" width="{{.FrameW}}" height="{{.FrameH}}" rx="6"/>
{{range .Items}}<g transform="translate({{.X}},{{.Y}})">{{.Body}}</g>
{{end}}</svg>`

type framedItem struct {
	X, Y int
	Body string
}

type frameData struct {
	Width, Height, FrameW, FrameH int
	Title, CSS                    string
	Items                         []framedItem
}

// NewFramer returns a Framer configured with the given options. Zero-valued
// option fields are replaced with sensible defaults.
func NewFramer(opts Options) *Framer {
	if opts.Width == 0 {
		opts.Width = 480
	}
	if opts.PadTop == 0 {
		opts.PadTop = 16
	}
	if opts.PadGap == 0 {
		opts.PadGap = 12
	}
	if opts.PadBot == 0 {
		opts.PadBot = 16
	}
	if opts.PadSide == 0 {
		opts.PadSide = 16
	}
	t := template.Must(template.New("frame").Parse(frameTmpl))
	return &Framer{opts: opts, tmpl: t}
}

// Compose stacks the given fragments vertically with padding and returns the
// final outer SVG as a string.
func (f *Framer) Compose(frags []plugin.Fragment) (string, error) {
	y := f.opts.PadTop
	items := make([]framedItem, 0, len(frags))
	for _, fr := range frags {
		items = append(items, framedItem{X: f.opts.PadSide, Y: y, Body: fr.Body})
		y += fr.Height + f.opts.PadGap
	}
	var height int
	if len(frags) == 0 {
		height = f.opts.PadTop + f.opts.PadBot
	} else {
		height = y - f.opts.PadGap + f.opts.PadBot
	}
	data := frameData{
		Width:  f.opts.Width,
		Height: height,
		FrameW: f.opts.Width - 1,
		FrameH: height - 1,
		Title:  f.opts.Title,
		CSS:    ClassicCSS,
		Items:  items,
	}
	var buf bytes.Buffer
	if err := f.tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute frame: %w", err)
	}
	return buf.String(), nil
}
