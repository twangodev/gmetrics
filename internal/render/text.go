package render

import (
	"fmt"
	"io"

	"github.com/tdewolff/canvas"
)

// DrawWrappedText lays out text in a box of the given maxWidth using
// Knuth-Plass line breaking, draws it at (x, y) on the context, and returns
// the consumed height (in canvas units, i.e. millimeters).
//
// The supplied face already carries a fill color from Face(); the explicit
// color argument is honored by setting it on the context before drawing so
// callers can override on a per-draw basis without rebuilding the face.
func DrawWrappedText(ctx *canvas.Context, x, y, maxWidth float64, face *canvas.FontFace, text string, color canvas.Paint) float64 {
	if color.Has() {
		ctx.SetFill(color)
	}
	rt := canvas.NewRichText(face)
	rt.WriteString(text)
	box := rt.ToText(maxWidth, 0, canvas.Left, canvas.Top, nil)
	ctx.DrawText(x, y, box)
	return box.Bounds().H()
}

// EmitTextPath writes a single line of text to w as an SVG <path> element
// (text-as-path) using the supplied font face. Glyphs are flipped on Y
// because canvas paths grow upward from the baseline while SVG grows
// downward. Empty strings are silently skipped.
//
// Use EmitTextPathClass to set a CSS class (e.g. "text-muted") on the
// emitted element.
func EmitTextPath(w io.Writer, x, baselineY int, s string, face *canvas.FontFace) {
	EmitTextPathClass(w, x, baselineY, s, face, "")
}

// EmitTextPathClass is the class-aware variant of EmitTextPath.
//
// When class is empty, the emitted <path> carries an explicit fill="#777"
// (the upstream classic-template body text colour). CSS `text { fill: … }`
// does NOT cascade into <path> elements, so without an explicit fill the
// glyph paths would render in the SVG default (black) regardless of the
// theme rules.
func EmitTextPathClass(w io.Writer, x, baselineY int, s string, face *canvas.FontFace, class string) {
	if s == "" {
		return
	}
	d := PathDataFor(face, s)
	if d == "" {
		return
	}
	attrs := ` fill="#777777"`
	if class != "" {
		attrs = fmt.Sprintf(` class="%s"`, class)
	}
	fmt.Fprintf(w, `<path%s transform="translate(%d,%d) scale(1,-1)" d="%s"/>`, attrs, x, baselineY, d)
}

// EmitTextPathColor emits text-as-path with an explicit fill color, for
// one-off accents that don't fit a CSS class.
func EmitTextPathColor(w io.Writer, x, baselineY int, s string, face *canvas.FontFace, fill string) {
	if s == "" {
		return
	}
	d := PathDataFor(face, s)
	if d == "" {
		return
	}
	if fill == "" {
		fill = "#777777"
	}
	fmt.Fprintf(w, `<path fill="%s" transform="translate(%d,%d) scale(1,-1)" d="%s"/>`, fill, x, baselineY, d)
}

// EmitTextPathRightAligned draws a line so its right edge sits at rightX.
// Useful for right-aligned stats values.
func EmitTextPathRightAligned(w io.Writer, rightX, baselineY int, s string, face *canvas.FontFace) {
	EmitTextPathRightAlignedClass(w, rightX, baselineY, s, face, "")
}

// EmitTextPathRightAlignedClass is the class-aware variant.
func EmitTextPathRightAlignedClass(w io.Writer, rightX, baselineY int, s string, face *canvas.FontFace, class string) {
	if s == "" {
		return
	}
	width := face.TextWidth(s)
	x := rightX - int(width+0.5)
	EmitTextPathClass(w, x, baselineY, s, face, class)
}

// PathDataFor converts a string to SVG path data using the supplied face.
// Returns the empty string when the font produces no glyph paths (e.g.
// the input is whitespace only); callers fall through gracefully.
func PathDataFor(face *canvas.FontFace, s string) string {
	p, _, err := face.ToPath(s)
	if err != nil || p == nil || p.Empty() {
		return ""
	}
	return p.ToSVG()
}

