package render_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tdewolff/canvas"
	"github.com/twangodev/gmetrics/internal/plugin"
	"github.com/twangodev/gmetrics/internal/render"
)

func TestCompose_BasicStackedFragments(t *testing.T) {
	const padTop, padGap, padBot = 16, 12, 16
	const firstHeight, secondHeight = 50, 30
	expectedHeight := padTop + firstHeight + padGap + secondHeight + padBot

	fr := render.NewFramer(render.Options{Width: 480})
	out, err := fr.Compose([]plugin.Fragment{
		{Body: `<rect width="200" height="50"/>`, Width: 200, Height: firstHeight},
		{Body: `<rect width="200" height="30"/>`, Width: 200, Height: secondHeight},
	})
	require.NoError(t, err)
	require.Contains(t, out, `width="480"`)
	require.Contains(t, out, "<style>")
	require.Contains(t, out, `viewBox="0 0 480`)
	require.Contains(t, out, fmt.Sprintf(`height="%d"`, expectedHeight))
}

func TestCompose_EmptyFragments(t *testing.T) {
	fr := render.NewFramer(render.Options{Width: 480})
	out, err := fr.Compose(nil)
	require.NoError(t, err)
	require.Contains(t, out, `width="480"`)
}

func TestCompose_IncludesThemeCSS(t *testing.T) {
	fr := render.NewFramer(render.Options{Width: 480, Title: "Test Card"})
	out, err := fr.Compose(nil)
	require.NoError(t, err)
	require.Contains(t, out, "fill: #777777")
	require.Contains(t, out, `aria-label="Test Card"`)
}

func TestCompose_FragmentBodiesAreEmittedInOrder(t *testing.T) {
	fr := render.NewFramer(render.Options{Width: 480})
	out, err := fr.Compose([]plugin.Fragment{
		{Body: `<rect id="alpha"/>`, Width: 200, Height: 30},
		{Body: `<rect id="beta"/>`, Width: 200, Height: 30},
	})
	require.NoError(t, err)
	alpha := strings.Index(out, "alpha")
	beta := strings.Index(out, "beta")
	require.Positive(t, alpha)
	require.Positive(t, beta)
	require.Less(t, alpha, beta)
}

func TestLoadFonts_RegularAndBold(t *testing.T) {
	regular, err := render.Face(14, canvas.FontRegular)
	require.NoError(t, err)
	require.NotNil(t, regular)
	bold, err := render.Face(14, canvas.FontBold)
	require.NoError(t, err)
	require.NotNil(t, bold)
}

func TestNewFragment_ReturnsCanvasAndContext(t *testing.T) {
	c, ctx := render.NewFragment(480)
	require.NotNil(t, c)
	require.NotNil(t, ctx)
	w, _ := c.Size()
	require.InDelta(t, 480.0, w, 0.001)
}

func TestDrawWrappedText_ReturnsNonZeroHeight(t *testing.T) {
	face, err := render.Face(14, canvas.FontRegular)
	require.NoError(t, err)
	_, ctx := render.NewFragment(480)
	h := render.DrawWrappedText(ctx, 0, 0, 400, face, "hello world", canvas.Paint{Color: canvas.Black})
	require.Greater(t, h, 0.0)
}
