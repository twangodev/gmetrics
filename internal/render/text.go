package render

import (
	"fmt"
	"io"
	"strings"

	"github.com/tdewolff/canvas"
)

const classicBodyTextFill = "#777777"

// DrawWrappedText returns the consumed height in canvas units (millimeters).
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

func EmitTextPath(w io.Writer, x, baselineY int, s string, face *canvas.FontFace) {
	EmitTextPathClass(w, x, baselineY, s, face, "")
}

// CSS `text { fill: … }` does not cascade into <path>, so an unclassed glyph
// path needs an explicit fill or it renders black instead of the theme color.
func EmitTextPathClass(w io.Writer, x, baselineY int, s string, face *canvas.FontFace, class string) {
	if s == "" {
		return
	}
	d := PathDataFor(face, s)
	if d == "" {
		return
	}
	attrs := fmt.Sprintf(` fill="%s"`, classicBodyTextFill)
	if class != "" {
		attrs = fmt.Sprintf(` class="%s"`, class)
	}
	fmt.Fprintf(w, `<path%s transform="translate(%d,%d) scale(1,-1)" d="%s"/>`, attrs, x, baselineY, d)
}

func EmitTextPathColor(w io.Writer, x, baselineY int, s string, face *canvas.FontFace, fill string) {
	if s == "" {
		return
	}
	d := PathDataFor(face, s)
	if d == "" {
		return
	}
	if fill == "" {
		fill = classicBodyTextFill
	}
	fmt.Fprintf(w, `<path fill="%s" transform="translate(%d,%d) scale(1,-1)" d="%s"/>`, fill, x, baselineY, d)
}

func EmitTextPathRightAligned(w io.Writer, rightX, baselineY int, s string, face *canvas.FontFace) {
	EmitTextPathRightAlignedClass(w, rightX, baselineY, s, face, "")
}

func EmitTextPathRightAlignedClass(w io.Writer, rightX, baselineY int, s string, face *canvas.FontFace, class string) {
	if s == "" {
		return
	}
	width := TextWidth(face, s)
	x := rightX - int(width+0.5)
	EmitTextPathClass(w, x, baselineY, s, face, class)
}

// radiusFrac is in objectBoundingBox units so one def rounds every referencing
// element identically regardless of size; 0.125 rounds a 32px box by 4px.
func EmitRoundedClip(w io.Writer, id string, radiusFrac float64) {
	fmt.Fprintf(w,
		`<clipPath id="%s" clipPathUnits="objectBoundingBox"><rect width="1" height="1" rx="%g" ry="%g"/></clipPath>`,
		id, radiusFrac, radiusFrac,
	)
}

func TruncateToWidth(s string, face *canvas.FontFace, maxW float64) string {
	if s == "" || TextWidth(face, s) <= maxW {
		return s
	}
	const ellipsis = "…"
	if TextWidth(face, ellipsis) > maxW {
		return ""
	}
	runes := []rune(s)
	lo, hi := 0, len(runes)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if TextWidth(face, string(runes[:mid])+ellipsis) <= maxW {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return string(runes[:lo]) + ellipsis
}

// fontRun is a maximal substring of a string that shares one face after
// splitting by glyph coverage.
type fontRun struct {
	face *canvas.FontFace
	text string
}

// faceFor picks the face that can draw r: the primary face when it has the
// glyph, otherwise the fallback face. Runes neither covers stay on the primary
// so its .notdef (tofu) renders as a last resort rather than vanishing.
func faceFor(primary, fallback *canvas.FontFace, r rune) *canvas.FontFace {
	if primary.Font.SFNT.GlyphIndex(r) != 0 {
		return primary
	}
	if fallback != nil && fallback.Font.SFNT.GlyphIndex(r) != 0 {
		return fallback
	}
	return primary
}

// splitRuns breaks s into consecutive runs, each drawn by a single face. The
// primary (Inter) keeps Latin and anything it covers; the fallback (Noto Sans
// KR) takes Hangul, kana, and CJK ideographs Inter lacks.
func splitRuns(primary, fallback *canvas.FontFace, s string) []fontRun {
	var runs []fontRun
	var b strings.Builder
	var cur *canvas.FontFace
	flush := func() {
		if b.Len() > 0 {
			runs = append(runs, fontRun{cur, b.String()})
			b.Reset()
		}
	}
	for _, r := range s {
		f := faceFor(primary, fallback, r)
		if cur != nil && f != cur {
			flush()
		}
		cur = f
		b.WriteRune(r)
	}
	flush()
	return runs
}

// TextWidth returns the rendered width of s, measuring each run with the face
// that actually draws it so CJK advances come from Noto, not Inter's .notdef.
func TextWidth(face *canvas.FontFace, s string) float64 {
	fallback := fallbackFaceFor(face)
	if fallback == nil {
		return face.TextWidth(s)
	}
	w := 0.0
	for _, run := range splitRuns(face, fallback, s) {
		w += run.face.TextWidth(run.text)
	}
	return w
}

// PathDataFor converts a string to SVG path data using the supplied face,
// falling back to Noto Sans KR for runes Inter cannot draw. Each run is laid
// out at the origin then translated by the accumulated advance so the runs sit
// left-to-right in a single path. Returns the empty string when the input
// produces no glyph paths (e.g. whitespace only); callers fall through
// gracefully.
//
// The output is run through SanitizePathData so a malformed command (e.g.
// a 5-arg cubic Bezier emitted by tdewolff/canvas when two coordinates
// fuse) doesn't poison the rest of the path in resvg / Chromium.
func PathDataFor(face *canvas.FontFace, s string) string {
	fallback := fallbackFaceFor(face)
	if fallback == nil {
		return pathDataSingle(face, s)
	}
	var b strings.Builder
	x := 0.0
	for _, run := range splitRuns(face, fallback, s) {
		if p, _, err := run.face.ToPath(run.text); err == nil && p != nil && !p.Empty() {
			if x != 0 {
				p = p.Translate(x, 0)
			}
			if d := SanitizePathData(p.ToSVG()); d != "" {
				if b.Len() > 0 {
					b.WriteByte(' ')
				}
				b.WriteString(d)
			}
		}
		x += run.face.TextWidth(run.text)
	}
	return b.String()
}

func pathDataSingle(face *canvas.FontFace, s string) string {
	p, _, err := face.ToPath(s)
	if err != nil || p == nil || p.Empty() {
		return ""
	}
	return SanitizePathData(p.ToSVG())
}

// Drops surplus trailing args so a malformed command (e.g. a 5-arg cubic Bezier
// emitted by tdewolff/canvas) doesn't poison the rest of the path in resvg.
func SanitizePathData(d string) string {
	if d == "" {
		return d
	}
	tokens := tokenizePath(d)
	arityByCommand := map[byte]int{
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
			arity := arityByCommand[toUpperByte(cmd)]
			if arity == 0 {
				b.WriteByte(cmd)
				continue
			}
			completeArgs := (len(args) / arity) * arity
			if completeArgs == 0 {
				continue
			}
			b.WriteByte(cmd)
			for j, a := range args[:completeArgs] {
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

// Per SVG parser convention a second '.' starts a new number.
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
