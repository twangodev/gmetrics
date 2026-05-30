package render

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/twangodev/gmetrics/internal/plugin"
)

type Options struct {
	Width                           int
	Title                           string
	PadTop, PadGap, PadBot, PadSide int
}

type Framer struct {
	opts Options
	tmpl *template.Template
}

// text/template, not html/template: fragment bodies are pre-sanitized SVG emitted verbatim.
const frameTmpl = `<svg xmlns="http://www.w3.org/2000/svg" width="{{.Width}}" height="{{.Height}}" viewBox="0 0 {{.Width}} {{.Height}}" role="img"{{if .Title}} aria-label="{{.Title}}"{{end}}>
<style>{{.CSS}}</style>
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

const (
	defaultWidth   = 480
	defaultPadTop  = 16
	defaultPadGap  = 12
	defaultPadBot  = 16
	defaultPadSide = 16
)

func NewFramer(opts Options) *Framer {
	if opts.Width == 0 {
		opts.Width = defaultWidth
	}
	if opts.PadTop == 0 {
		opts.PadTop = defaultPadTop
	}
	if opts.PadGap == 0 {
		opts.PadGap = defaultPadGap
	}
	if opts.PadBot == 0 {
		opts.PadBot = defaultPadBot
	}
	if opts.PadSide == 0 {
		opts.PadSide = defaultPadSide
	}
	t := template.Must(template.New("frame").Parse(frameTmpl))
	return &Framer{opts: opts, tmpl: t}
}

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
