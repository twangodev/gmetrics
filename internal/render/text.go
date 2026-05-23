package render

import (
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
