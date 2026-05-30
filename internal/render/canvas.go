// Package render composes plugin SVG fragments into the final outer SVG.
//
// Glyphs are converted to outline paths so output survives GitHub's Camo
// sanitizer, which strips foreignObject, scripts, and external font references.
package render

import (
	"embed"
	"fmt"
	"sync"

	"github.com/tdewolff/canvas"
)

//go:embed fonts/Inter-Regular.otf fonts/Inter-Bold.otf
var fontFS embed.FS

var (
	fontOnce   sync.Once
	fontFamily *canvas.FontFamily
	fontErr    error
)

func loadFonts() (*canvas.FontFamily, error) {
	fontOnce.Do(func() {
		ff := canvas.NewFontFamily("Inter")
		reg, err := fontFS.ReadFile("fonts/Inter-Regular.otf")
		if err != nil {
			fontErr = fmt.Errorf("read inter regular: %w", err)
			return
		}
		if err := ff.LoadFont(reg, 0, canvas.FontRegular); err != nil {
			fontErr = fmt.Errorf("load inter regular: %w", err)
			return
		}
		bold, err := fontFS.ReadFile("fonts/Inter-Bold.otf")
		if err != nil {
			fontErr = fmt.Errorf("read inter bold: %w", err)
			return
		}
		if err := ff.LoadFont(bold, 0, canvas.FontBold); err != nil {
			fontErr = fmt.Errorf("load inter bold: %w", err)
			return
		}
		fontFamily = ff
	})
	return fontFamily, fontErr
}

// canvas.Face takes points and converts to mm; our SVG maps 1mm to 1px, so px sizes need points = px * 72/25.4.
const ptPerPx = 72.0 / 25.4

func Face(sizePx float64, style canvas.FontStyle) (*canvas.FontFace, error) {
	ff, err := loadFonts()
	if err != nil {
		return nil, err
	}
	return ff.Face(sizePx*ptPerPx, canvas.Black, style, canvas.FontNormal), nil
}

// fragmentScratchHeight is over-tall so any drawn content fits; callers call c.Fit before export to trim to actual bounds.
const fragmentScratchHeight = 10000

func NewFragment(maxWidth float64) (*canvas.Canvas, *canvas.Context) {
	c := canvas.New(maxWidth, fragmentScratchHeight)
	ctx := canvas.NewContext(c)
	return c, ctx
}
