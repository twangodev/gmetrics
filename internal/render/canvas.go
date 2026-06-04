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

//go:embed fonts/Inter-Regular.otf fonts/Inter-Bold.otf fonts/NotoSansKR-Regular.otf fonts/NotoSansKR-Bold.otf
var fontFS embed.FS

var (
	fontOnce   sync.Once
	fontFamily *canvas.FontFamily
	fontErr    error

	fallbackOnce   sync.Once
	fallbackFamily *canvas.FontFamily
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

// Face returns a canvas font face at the given size **in pixels**.
//
// The underlying canvas.FontFamily.Face accepts a size in *points* and
// converts to mm internally (Size = sizePt * mmPerPt, where mmPerPt =
// 25.4/72). Our SVG output maps 1 canvas mm to 1 SVG user unit (1 px),
// so a caller asking for "14 px text" needs us to pass sizePt = 14 *
// 72/25.4 ≈ 39.7 pt to canvas. ptPerPx encapsulates that conversion.
const ptPerPx = 72.0 / 25.4

func Face(sizePx float64, style canvas.FontStyle) (*canvas.FontFace, error) {
	ff, err := loadFonts()
	if err != nil {
		return nil, err
	}
	return ff.Face(sizePx*ptPerPx, canvas.Black, style, canvas.FontNormal), nil
}

// loadFallbackFonts parses Noto Sans KR Regular + Bold into a family used to
// draw runes Inter lacks (Hangul, kana, and CJK ideographs). A load failure
// leaves the family nil so callers degrade to Inter's .notdef rather than
// crashing — fallback is best-effort, not load-bearing for the Latin path.
func loadFallbackFonts() *canvas.FontFamily {
	fallbackOnce.Do(func() {
		ff := canvas.NewFontFamily("NotoSansKR")
		reg, err := fontFS.ReadFile("fonts/NotoSansKR-Regular.otf")
		if err != nil {
			return
		}
		if err := ff.LoadFont(reg, 0, canvas.FontRegular); err != nil {
			return
		}
		bold, err := fontFS.ReadFile("fonts/NotoSansKR-Bold.otf")
		if err != nil {
			return
		}
		if err := ff.LoadFont(bold, 0, canvas.FontBold); err != nil {
			return
		}
		fallbackFamily = ff
	})
	return fallbackFamily
}

// fallbackFaceFor returns a Noto face matching primary's pixel size and style,
// or nil when the fallback family is unavailable. primary.Size is in mm, which
// equals pixels under our 1-mm-per-unit mapping, so it converts back to points
// the same way Face does.
func fallbackFaceFor(primary *canvas.FontFace) *canvas.FontFace {
	fam := loadFallbackFonts()
	if fam == nil {
		return nil
	}
	return fam.Face(primary.Size*ptPerPx, canvas.Black, primary.Style, canvas.FontNormal)
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
