package render_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tdewolff/canvas"
	"github.com/twangodev/gmetrics/internal/render"
)

// Inter has no CJK glyphs, so without font fallback every CJK codepoint
// collapses to the same .notdef tofu box. These syllables must render as
// three distinct glyph paths once Noto Sans KR backs the missing runes.
func TestPathDataFor_KoreanGlyphsAreDistinct(t *testing.T) {
	face, err := render.Face(14, canvas.FontRegular)
	require.NoError(t, err)

	an := render.PathDataFor(face, "안")
	nyeong := render.PathDataFor(face, "녕")
	ha := render.PathDataFor(face, "하")

	require.NotEmpty(t, an)
	require.NotEqual(t, an, nyeong, "distinct Hangul syllables must not share a glyph (tofu)")
	require.NotEqual(t, an, ha)
	require.NotEqual(t, nyeong, ha)
}

// A mixed Latin+Hangul string must keep the Latin glyphs from Inter and the
// Hangul glyphs from the fallback, positioned left-to-right in one path.
func TestPathDataFor_MixedScriptRendersBothScripts(t *testing.T) {
	face, err := render.Face(14, canvas.FontRegular)
	require.NoError(t, err)

	latin := render.PathDataFor(face, "Hi")
	mixed := render.PathDataFor(face, "Hi 안녕")

	require.NotEmpty(t, latin)
	require.Greater(t, len(mixed), len(latin), "mixed string must contain extra Hangul geometry")
}

// TextWidth must measure CJK runs with the fallback face, not Inter's
// .notdef advance, so truncation and alignment stay correct.
func TestTextWidth_CJKUsesFallbackAdvance(t *testing.T) {
	face, err := render.Face(14, canvas.FontRegular)
	require.NoError(t, err)

	w := render.TextWidth(face, "안녕하세요")
	require.Greater(t, w, 0.0)
	require.InDelta(t, render.TextWidth(face, "Hello")+w, render.TextWidth(face, "Hello안녕하세요"), 0.01)
}
