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

// 440px content area inside the engine's 480px card (16px frame padding each side).
const fragmentWidth = 440

const (
	h2BaselineY = 16
	h3BaselineY = 36
	headerBlock = 48
	rowHeight   = 40
	artworkSize = 32
	textIndent  = artworkSize + 12
	iconSize    = 16
	iconGutter  = 8

	columns         = 2
	columnGap       = 16
	columnWidth     = (fragmentWidth - columnGap*(columns-1)) / columns
	textBaselineGap = 12
)

const (
	artworkCornerFrac = 0.125
	artClipID         = "music-art-round"
)

func (*Plugin) Render(env *plugin.Env, raw any) (plugin.Fragment, error) {
	data, ok := raw.(Data)
	if !ok {
		return plugin.Fragment{}, fmt.Errorf("music: render: want Data, got %T", raw)
	}

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

	render.EmitRoundedClip(&buf, artClipID, artworkCornerFrac)

	render.EmitOcticon(&buf, 0, 0, iconSize, "music-note", "#0366d6")
	render.EmitTextPathClass(&buf, iconSize+iconGutter, h2BaselineY, modeLabel(data.Mode), h2Face, "text-heading")

	render.EmitOcticon(&buf, 0, h3BaselineY-iconSize+2, iconSize, "broadcast", "#959da5")
	render.EmitTextPathClass(&buf, iconSize+iconGutter, h3BaselineY, "From "+providerLabel(data.Provider), h3Face, "text-muted")

	nameBaseY, artistBaseY := centeredTextBaselines(trackNameFace.Metrics())

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
		if p == "" {
			return ""
		}
		return strings.ToUpper(p[:1]) + p[1:]
	}
}

func writeRow(buf *bytes.Buffer, t Track, x, y, nameBaseY, artistBaseY int, nameFace, artistFace *canvas.FontFace) {
	fmt.Fprintf(buf, `<g class="music-row" transform="translate(%d,%d)">`, x, y)

	if t.ArtworkB64 != "" {
		fmt.Fprintf(buf,
			`<image x="0" y="0" width="%d" height="%d" href="%s" clip-path="url(#%s)"><title>%s</title></image>`,
			artworkSize, artworkSize, xmlEscapeAttr(t.ArtworkB64), artClipID, xmlEscape(t.Name),
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

func centeredTextBaselines(nm canvas.FontMetrics) (int, int) {
	block := textBaselineGap + nm.CapHeight
	nameY := math.Round((float64(artworkSize)-block)/2 + nm.CapHeight)
	return int(nameY), int(nameY) + textBaselineGap
}

func xmlEscape(s string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(s))
	return buf.String()
}

func xmlEscapeAttr(s string) string {
	s = xmlEscape(s)
	// xml.EscapeText leaves '"' untouched; without this a payload could close the attribute.
	s = strings.ReplaceAll(s, `"`, `&quot;`)
	return s
}
