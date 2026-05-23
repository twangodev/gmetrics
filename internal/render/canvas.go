// Package render is the central rendering layer for gmetrics. It bundles a
// font (Inter), exposes helpers plugins use to lay out text and build SVG
// fragments, and composes those fragments into the final outer SVG.
//
// The rendering pipeline is pure-SVG with glyphs converted to outline paths.
// This is a deliberate choice to keep the output stable when proxied through
// GitHub's Camo image sanitizer, which strips foreignObject, scripts, and
// external font references.
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

// loadFonts parses Inter Regular + Bold into a canvas.FontFamily exactly once.
// It is safe to call concurrently; the first call performs the work and
// subsequent calls observe the cached family (or the cached error).
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

// Face returns a canvas font face at the given size in points and weight
// (canvas.FontRegular or canvas.FontBold). The returned *canvas.FontFace is
// what tdewolff/canvas APIs such as canvas.NewRichText accept; the spec
// originally described this as *canvas.Font but the live API uses FontFace.
func Face(sizePt float64, style canvas.FontStyle) (*canvas.FontFace, error) {
	ff, err := loadFonts()
	if err != nil {
		return nil, err
	}
	return ff.Face(sizePt, canvas.Black, style, canvas.FontNormal), nil
}

// NewFragment creates a virtual canvas that a plugin draws onto and a context
// over it. The canvas is initialized with the supplied maxWidth and an
// intentionally over-tall height; plugins should call c.Fit(margin) before
// exporting the fragment so its bounds reflect actual drawn content.
//
// Units are millimeters at the canvas API layer; when exported to SVG the
// numeric values map to pixels at 1 DPMM, which is what plugins and the
// frame composer assume throughout this package.
func NewFragment(maxWidth float64) (*canvas.Canvas, *canvas.Context) {
	c := canvas.New(maxWidth, 10000)
	ctx := canvas.NewContext(c)
	return c, ctx
}
