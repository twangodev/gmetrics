package music

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/twangodev/gmetrics/internal/plugin"
)

// fragmentWidth is the working width every plugin draws against. It
// matches the 440px content area used by the engine for a 480px-wide card
// (16px frame padding on each side; see the people plugin for the same
// constant).
const fragmentWidth = 440

// Vertical metrics for the music card. Header is rendered as a single
// 14pt bold text line; each track row is 40px tall (32px artwork + 4px
// breathing room top/bottom) with a fixed gap between rows. These
// constants are also used to compute the final fragment height, which
// must be exact for the frame composer to position adjacent fragments
// correctly.
const (
	headerHeight = 28
	rowHeight    = 40
	artworkSize  = 32
	textIndent   = artworkSize + 12 // 12px gap between artwork and text
)

// Render lays out the music card: a "Recently Played · Last.fm" header
// followed by one row per track. Each row draws the artwork (or a
// placeholder rectangle when none is available) on the left and a stack
// of track name / artist / played-at text on the right.
func (*Plugin) Render(env *plugin.Env, raw any) (plugin.Fragment, error) {
	data, ok := raw.(Data)
	if !ok {
		return plugin.Fragment{}, fmt.Errorf("music: render: want Data, got %T", raw)
	}

	var buf bytes.Buffer

	// Header: "Recently Played · Last.fm" at 14pt bold. We use the same
	// baseline-at-18 convention as the people plugin so the header band
	// has comparable visual weight across cards.
	title := headerTitle(data)
	fmt.Fprintf(&buf,
		`<text x="0" y="18" font-size="14" font-weight="600" fill="var(--color-text)">%s</text>`,
		xmlEscape(title),
	)

	// Track rows. Each row occupies rowHeight; the per-row layout already
	// includes its top/bottom breathing room so adjacent rows can sit
	// flush. The +8 at the bottom (added below) matches the spec's height
	// formula and gives the final row visual separation from whatever
	// fragment follows.
	for i, t := range data.Tracks {
		y := headerHeight + i*rowHeight
		writeRow(&buf, t, y)
	}

	// Height formula taken verbatim from the spec:
	//   28 (header) + len(Tracks) * 40 (row) + 8 (trailing pad).
	height := headerHeight + len(data.Tracks)*rowHeight + 8

	return plugin.Fragment{
		Body:   buf.String(),
		Width:  fragmentWidth,
		Height: height,
	}, nil
}

// headerTitle composes the section header. Mode and Provider come from
// Data so the same renderer can in principle handle other modes/providers
// later without touching Config plumbing. The v1 scope only ever feeds
// "recent" + "lastfm", giving "Recently Played · Last.fm".
func headerTitle(d Data) string {
	mode := "Recently Played"
	switch d.Mode {
	case "recent":
		mode = "Recently Played"
	case "top":
		mode = "Top Played"
	case "playlist":
		mode = "Playlist"
	}
	provider := "Last.fm"
	switch d.Provider {
	case "lastfm":
		provider = "Last.fm"
	case "spotify":
		provider = "Spotify"
	case "apple":
		provider = "Apple Music"
	case "youtube":
		provider = "YouTube Music"
	}
	return fmt.Sprintf("%s · %s", mode, provider)
}

// writeRow emits one track row at the given y offset. The artwork is an
// <image> when the base64 URL is non-empty, otherwise a muted rectangle
// the same size so the layout doesn't shift in test mode (env.HTTP nil).
func writeRow(buf *bytes.Buffer, t Track, y int) {
	fmt.Fprintf(buf, `<g class="music-row" transform="translate(0,%d)">`, y)

	// Artwork
	if t.ArtworkB64 != "" {
		fmt.Fprintf(buf,
			`<image x="0" y="0" width="%d" height="%d" href="%s"><title>%s</title></image>`,
			artworkSize, artworkSize, xmlEscapeAttr(t.ArtworkB64), xmlEscape(t.Name),
		)
	} else {
		fmt.Fprintf(buf,
			`<rect x="0" y="0" width="%d" height="%d" rx="4" fill="var(--color-border)"><title>%s</title></rect>`,
			artworkSize, artworkSize, xmlEscape(t.Name),
		)
	}

	// Track name: 12pt bold, baseline at 12 so the descender doesn't
	// collide with the artist line below.
	fmt.Fprintf(buf,
		`<text x="%d" y="12" font-size="12" font-weight="600" fill="var(--color-text)">%s</text>`,
		textIndent, xmlEscape(t.Name),
	)
	// Artist: 11pt muted, baseline at 24.
	fmt.Fprintf(buf,
		`<text x="%d" y="24" font-size="11" fill="var(--color-muted)">%s</text>`,
		textIndent, xmlEscape(t.Artist),
	)
	// PlayedAt: 10pt muted, baseline at 35. Skipped when empty so we
	// don't emit a stray empty <text/> for tracks without timestamps.
	if t.PlayedAt != "" {
		fmt.Fprintf(buf,
			`<text x="%d" y="35" font-size="10" fill="var(--color-muted)">%s</text>`,
			textIndent, xmlEscape(t.PlayedAt),
		)
	}

	fmt.Fprint(buf, `</g>`)
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
