package render

import (
	"fmt"
	"io"
	"strings"

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

// EmitRoundedClip writes a reusable <clipPath> that rounds the corners of
// any element referencing it via clip-path="url(#id)". The radius is given
// in objectBoundingBox units (a fraction of the referencing element's own
// bounding box), so one def rounds every image identically regardless of
// each image's size or absolute position — useful for lists of same-shaped
// thumbnails. radiusFrac of 0.125 rounds a 32px box by 4px.
func EmitRoundedClip(w io.Writer, id string, radiusFrac float64) {
	fmt.Fprintf(w,
		`<clipPath id="%s" clipPathUnits="objectBoundingBox"><rect width="1" height="1" rx="%g" ry="%g"/></clipPath>`,
		id, radiusFrac, radiusFrac,
	)
}

// TruncateToWidth returns s clipped (rune-aware) so its rendered width
// under face fits within maxW pixels, appending an ellipsis when clipped.
// Binary-searches the largest prefix that still fits with the ellipsis.
func TruncateToWidth(s string, face *canvas.FontFace, maxW float64) string {
	if s == "" || face.TextWidth(s) <= maxW {
		return s
	}
	const ellipsis = "…"
	if face.TextWidth(ellipsis) > maxW {
		return ""
	}
	runes := []rune(s)
	lo, hi := 0, len(runes)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if face.TextWidth(string(runes[:mid])+ellipsis) <= maxW {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return string(runes[:lo]) + ellipsis
}

// PathDataFor converts a string to SVG path data using the supplied face.
// Returns the empty string when the font produces no glyph paths (e.g.
// the input is whitespace only); callers fall through gracefully.
//
// The output is run through SanitizePathData so a malformed command (e.g.
// a 5-arg cubic Bezier emitted by tdewolff/canvas when two coordinates
// fuse) doesn't poison the rest of the path in resvg / Chromium.
func PathDataFor(face *canvas.FontFace, s string) string {
	p, _, err := face.ToPath(s)
	if err != nil || p == nil || p.Empty() {
		return ""
	}
	return SanitizePathData(p.ToSVG())
}

// SanitizePathData rewrites SVG path data so each command's argument
// count is a whole multiple of the expected per-command arity. Surplus
// trailing args are dropped; commands with zero complete argument
// groups are removed entirely.
func SanitizePathData(d string) string {
	if d == "" {
		return d
	}
	tokens := tokenizePath(d)
	argCount := map[byte]int{
		'M': 2, 'L': 2, 'H': 1, 'V': 1, 'Z': 0,
		'C': 6, 'S': 4, 'Q': 4, 'T': 2, 'A': 7,
	}
	var b strings.Builder
	i := 0
	for i < len(tokens) {
		tok := tokens[i]
		if len(tok) == 1 && isPathCmd(tok[0]) {
			cmd := tok[0]
			i++
			args := []string{}
			for i < len(tokens) && !(len(tokens[i]) == 1 && isPathCmd(tokens[i][0])) {
				args = append(args, tokens[i])
				i++
			}
			ac := argCount[toUpperByte(cmd)]
			if ac == 0 {
				b.WriteByte(cmd)
				continue
			}
			full := (len(args) / ac) * ac
			if full == 0 {
				continue
			}
			b.WriteByte(cmd)
			for j, a := range args[:full] {
				if j > 0 {
					b.WriteByte(' ')
				}
				b.WriteString(a)
			}
		} else {
			i++
		}
	}
	return b.String()
}

func isPathCmd(c byte) bool {
	switch c {
	case 'M', 'm', 'L', 'l', 'H', 'h', 'V', 'v', 'Z', 'z',
		'C', 'c', 'S', 's', 'Q', 'q', 'T', 't', 'A', 'a':
		return true
	}
	return false
}

func toUpperByte(c byte) byte {
	if c >= 'a' && c <= 'z' {
		return c - 32
	}
	return c
}

// tokenizePath splits SVG path data into command letters and decimal
// number strings, following SVG parser conventions (a second '.' starts
// a new number).
func tokenizePath(d string) []string {
	out := []string{}
	n := len(d)
	i := 0
	for i < n {
		c := d[i]
		switch {
		case c == ' ' || c == ',' || c == '\t' || c == '\n' || c == '\r':
			i++
		case isPathCmd(c):
			out = append(out, string(c))
			i++
		default:
			start := i
			if c == '+' || c == '-' {
				i++
			}
			sawDot := false
			for i < n {
				cc := d[i]
				switch {
				case cc >= '0' && cc <= '9':
					i++
				case cc == '.':
					if sawDot {
						goto done
					}
					sawDot = true
					i++
				case cc == 'e' || cc == 'E':
					i++
					if i < n && (d[i] == '+' || d[i] == '-') {
						i++
					}
				default:
					goto done
				}
			}
		done:
			if i > start {
				out = append(out, d[start:i])
			} else {
				i++
			}
		}
	}
	return out
}

