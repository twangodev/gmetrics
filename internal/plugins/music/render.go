package music

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"math"
	"strings"

	"github.com/tdewolff/canvas"
	"github.com/twangodev/gmetrics/internal/plugin"
	"github.com/twangodev/gmetrics/internal/render"
)

// fragmentWidth is the working width every plugin draws against. It
// matches the 440px content area used by the engine for a 480px-wide card
// (16px frame padding on each side; see the people plugin for the same
// constant).
const fragmentWidth = 440

// Vertical metrics. The header band mirrors upstream metrics's classic
// template: a 16px h2 ("Recently played") followed by a 12px h3 ("From
// Last.fm"). Tracks render in a 2-column grid with 40px-tall rows so the
// 32px album artwork has breathing room above and below; each column is
// columnWidth wide with a columnGap between them. These constants are also
// used to compute the final fragment height, which must be exact for the
// frame composer to position adjacent fragments correctly.
const (
	h2BaselineY = 16
	h3BaselineY = 36
	headerBlock = 48
	rowHeight   = 40
	artworkSize = 32
	textIndent  = artworkSize + 12
	iconSize    = 16
	iconGutter  = 8

	columns          = 2
	columnGap        = 16
	columnWidth      = (fragmentWidth - columnGap*(columns-1)) / columns
	textBaselineGap  = 12
)

// Render lays out the music card: an h2 + h3 header pair followed by one
// row per track. The header mirrors upstream lowlighter/metrics's classic
// template — a music-note icon next to "Recently played" (h2), with a
// "From <Provider>" subheader below in a smaller, muted face.
func (*Plugin) Render(env *plugin.Env, raw any) (plugin.Fragment, error) {
	data, ok := raw.(Data)
	if !ok {
		return plugin.Fragment{}, fmt.Errorf("music: render: want Data, got %T", raw)
	}

	// Build all font faces once. Upstream's classic CSS pins h2 at 16px
	// regular, h3 at 14px regular, the track name at 14px semibold and the
	// artist/played-at lines at ~12px regular muted. We approximate
	// "semibold" with our bundled Bold face, which is the only bold variant
	// shipped with the Inter subset.
	h2Face, err := render.Face(16, canvas.FontRegular)
	if err != nil {
		return plugin.Fragment{}, fmt.Errorf("music: load h2 face: %w", err)
	}
	h3Face, err := render.Face(12, canvas.FontRegular)
	if err != nil {
		return plugin.Fragment{}, fmt.Errorf("music: load h3 face: %w", err)
	}
	trackNameFace, err := render.Face(12, canvas.FontBold)
	if err != nil {
		return plugin.Fragment{}, fmt.Errorf("music: load track-name face: %w", err)
	}
	artistFace, err := render.Face(11, canvas.FontRegular)
	if err != nil {
		return plugin.Fragment{}, fmt.Errorf("music: load artist face: %w", err)
	}

	var buf bytes.Buffer

	// h2: "<music-note> Recently played" in the heading color.
	render.EmitOcticon(&buf, 0, 0, iconSize, "music-note", "#0366d6")
	render.EmitTextPathClass(&buf, iconSize+iconGutter, h2BaselineY, modeLabel(data.Mode), h2Face, "text-heading")

	// h3: "<broadcast> From <Provider>" muted, smaller.
	render.EmitOcticon(&buf, 0, h3BaselineY-iconSize+2, iconSize, "broadcast", "#959da5")
	render.EmitTextPathClass(&buf, iconSize+iconGutter, h3BaselineY, "From "+providerLabel(data.Provider), h3Face, "text-muted")

	// Vertically center the name+artist text block against the artwork.
	// The "visible block" we center spans cap-top of the name line down to
	// the artist baseline (descenders ignored — most artist names lack
	// them, and including them would visually lift the block).
	nameBaseY, artistBaseY := centeredTextBaselines(trackNameFace.Metrics())

	// Track grid. Tracks fill the 2-column grid in row-major reading order
	// (1,2 / 3,4 / ...), starting directly below the header block. The
	// per-row layout already includes top/bottom breathing room so rows can
	// sit flush against each other.
	for i, t := range data.Tracks {
		row, col := i/columns, i%columns
		x := col * (columnWidth + columnGap)
		y := headerBlock + row*rowHeight
		writeRow(&buf, t, x, y, nameBaseY, artistBaseY, trackNameFace, artistFace)
	}

	rows := (len(data.Tracks) + columns - 1) / columns
	height := headerBlock + rows*rowHeight + 8

	return plugin.Fragment{
		Body:   buf.String(),
		Width:  fragmentWidth,
		Height: height,
	}, nil
}

// modeLabel returns the user-facing string for the upstream "mode" field.
// v1 only ships "recent" but we keep the switch so adding a new mode is a
// trivial edit instead of a wider refactor.
func modeLabel(mode string) string {
	switch mode {
	case "top":
		return "Top played"
	case "playlist":
		return "Playlist"
	case "recent":
		fallthrough
	default:
		return "Recently played"
	}
}

// providerLabel formats the provider key for display. The Last.fm name is
// the only provider with internal capitalisation, but Spotify/Apple/YouTube
// are listed here too so future modes pick up the same display strings.
func providerLabel(p string) string {
	switch p {
	case "lastfm":
		return "Last.fm"
	case "spotify":
		return "Spotify"
	case "apple":
		return "Apple Music"
	case "youtube":
		return "YouTube Music"
	default:
		// Capitalise the first rune as a safe fallback so unknown providers
		// at least render as a Title-Case word rather than the raw lower
		// identifier.
		if p == "" {
			return ""
		}
		return strings.ToUpper(p[:1]) + p[1:]
	}
}

// writeRow emits one track at the given (x, y) offset. The artwork is an
// <image> when the base64 URL is non-empty, otherwise a muted rectangle
// the same size so the layout doesn't shift in test mode (env.HTTP nil).
// Name and artist are pixel-truncated to fit the column's text area and
// share the caller-supplied baselines (centered against the artwork).
func writeRow(buf *bytes.Buffer, t Track, x, y, nameBaseY, artistBaseY int, nameFace, artistFace *canvas.FontFace) {
	fmt.Fprintf(buf, `<g class="music-row" transform="translate(%d,%d)">`, x, y)

	if t.ArtworkB64 != "" {
		fmt.Fprintf(buf,
			`<image x="0" y="0" width="%d" height="%d" href="%s"><title>%s</title></image>`,
			artworkSize, artworkSize, xmlEscapeAttr(t.ArtworkB64), xmlEscape(t.Name),
		)
	} else {
		fmt.Fprintf(buf,
			`<rect x="0" y="0" width="%d" height="%d" rx="4" fill="#d0d7de"><title>%s</title></rect>`,
			artworkSize, artworkSize, xmlEscape(t.Name),
		)
	}

	textWidth := float64(columnWidth - textIndent)
	render.EmitTextPath(buf, textIndent, nameBaseY, render.TruncateToWidth(t.Name, nameFace, textWidth), nameFace)
	render.EmitTextPathClass(buf, textIndent, artistBaseY, render.TruncateToWidth(t.Artist, artistFace, textWidth), artistFace, "text-muted")

	fmt.Fprint(buf, `</g>`)
}

// centeredTextBaselines returns the (name, artist) baseline y-coordinates
// such that the cap-top of the name line and the baseline of the artist
// line are symmetric around the artwork's vertical center.
func centeredTextBaselines(nm canvas.FontMetrics) (int, int) {
	block := textBaselineGap + nm.CapHeight
	nameY := math.Round((float64(artworkSize)-block)/2 + nm.CapHeight)
	return int(nameY), int(nameY) + textBaselineGap
}

// xmlEscape escapes content for use as text inside an SVG element. This
// mirrors the helper in the people plugin; we don't share it because the
// internal/plugin package's xmlEscape is unexported.
func xmlEscape(s string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(s))
	return buf.String()
}

// xmlEscapeAttr escapes content for use as an SVG attribute value. The
// extra Replace covers '"' which xml.EscapeText leaves untouched (it
// targets text content, not attribute content); without it a base64
// payload containing a literal '"' would terminate the attribute early.
func xmlEscapeAttr(s string) string {
	s = xmlEscape(s)
	s = strings.ReplaceAll(s, `"`, `&quot;`)
	return s
}
