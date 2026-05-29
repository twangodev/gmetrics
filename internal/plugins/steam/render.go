package steam

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/tdewolff/canvas"
	"github.com/twangodev/gmetrics/internal/plugin"
	"github.com/twangodev/gmetrics/internal/render"
)

// fragmentWidth matches the working width every plugin draws against
// (440 px content inside a 480 px frame, with the engine's standard side
// padding).
const fragmentWidth = 440

// avatarSize is the side length of the player avatar.
const avatarSize = 48

// gameIconSize is the side length of the game icon inside a list row.
const gameIconSize = 32

// gameIconCornerFrac softens the game-icon corners (a fraction of the 32px
// box, so ~4px) so the thumbnails aren't razor-sharp squares. gameIconClipID
// is the shared clipPath id referenced by every icon <image>.
const (
	gameIconCornerFrac = 0.125
	gameIconClipID     = "steam-icon-round"
)

// Vertical spacing constants shared by every section. iconSize is the
// width and height of every inline octicon glyph in this plugin; iconGutter
// is the gap between an icon and the text immediately to its right. These
// match the values used by the music and wakatime plugins so the cards
// line up vertically when stacked by the framer.
const (
	iconSize   = 16
	iconGutter = 8

	h2BaselineY = 16
	h2BlockH    = 24 // h2 occupies 24 px (icon + 16px baseline + a little air)
	fieldLineH  = 20 // each prose-style field row is 20 px tall
	sectionGap  = 8  // gap between major sections
)

// Render lays out the steam card as upstream metrics does: a top-level h2
// ("Steam") followed by a two-column player block (name+games | level+hours)
// and then one block per requested game list. Each game list is itself a
// nested h2 plus a stack of game cards, each card carrying an icon and a
// short prose summary (playtime / last played / achievements when known).
func (*Plugin) Render(env *plugin.Env, raw any) (plugin.Fragment, error) {
	data, ok := raw.(Data)
	if !ok {
		return plugin.Fragment{}, fmt.Errorf("steam: render: want Data, got %T", raw)
	}
	if len(data.Sections) == 0 {
		// Default to the documented user config when callers forget to
		// propagate Sections from Config; keeps the fragment renderable
		// in standalone tests.
		data.Sections = []string{"player", "most-played", "recently-played"}
	}

	// Build every font face exactly once.
	h2Face, err := render.Face(16, canvas.FontRegular)
	if err != nil {
		return plugin.Fragment{}, fmt.Errorf("steam: load h2 face: %w", err)
	}
	fieldFace, err := render.Face(12, canvas.FontRegular)
	if err != nil {
		return plugin.Fragment{}, fmt.Errorf("steam: load field face: %w", err)
	}
	gameNameFace, err := render.Face(14, canvas.FontBold)
	if err != nil {
		return plugin.Fragment{}, fmt.Errorf("steam: load game-name face: %w", err)
	}

	var buf bytes.Buffer
	y := 0

	// Rounded-corner clip shared by every game icon in this card.
	render.EmitRoundedClip(&buf, gameIconClipID, gameIconCornerFrac)

	// Top-level h2 — "Steam" with the upstream broadcast-tower-shaped icon.
	// We fall back to the `broadcast` octicon (added for the music card) so
	// the header has an icon glyph next to it.
	render.EmitOcticon(&buf, 0, y, iconSize, "broadcast", "#0366d6")
	render.EmitTextPathClass(&buf, iconSize+iconGutter, y+h2BaselineY, "Steam", h2Face, "text-heading")
	y += h2BlockH

	for _, section := range data.Sections {
		switch section {
		case "player":
			h := writePlayer(&buf, data.Player, y, fieldFace)
			y += h
		case "most-played":
			h := writeGameList(&buf, "Most played", data.MostPlayed, y, h2Face, gameNameFace, fieldFace)
			y += h
		case "recently-played":
			h := writeGameList(&buf, "Recently played", data.Recently, y, h2Face, gameNameFace, fieldFace)
			y += h
		}
	}

	return plugin.Fragment{
		Body:   buf.String(),
		Width:  fragmentWidth,
		Height: y,
	}, nil
}

// writePlayer emits the player block: persona name + level + games + hours
// laid out as four icon+text fields in two columns. Returns the consumed
// vertical pixels.
func writePlayer(buf *bytes.Buffer, p Player, y int, fieldFace *canvas.FontFace) int {
	fmt.Fprintf(buf, `<g class="steam-player" transform="translate(0,%d)">`, y)

	name := p.Name
	if name == "" {
		name = "Unknown"
	}

	colWidth := fragmentWidth / 2
	// Column 1: persona name + games count.
	writeField(buf, 0, 0, "person", name, fieldFace)
	writeField(buf, 0, fieldLineH, "package", fmt.Sprintf("%d game%s", p.TotalGames, pluralInt(p.TotalGames)), fieldFace)
	// Column 2: steam level + total playtime. Hours are formatted to mirror
	// upstream metrics's `f(parseInt(...))`: integer rounding plus a "k"
	// suffix for thousands so the field fits in a single line at 12px.
	writeField(buf, colWidth, 0, "star", fmt.Sprintf("Steam level %d", p.Level), fieldFace)
	writeField(buf, colWidth, fieldLineH, "clock", fmt.Sprintf("%s hour%s played", formatHours(p.TotalHours), pluralHours(p.TotalHours)), fieldFace)

	fmt.Fprint(buf, `</g>`)
	// Two rows tall plus a small gap before the next section.
	return 2*fieldLineH + sectionGap
}

// writeField emits a single icon+text "field" row at (x, y). The icon is
// vertically centred against the 12px text baseline used by fieldFace.
func writeField(buf *bytes.Buffer, x, y int, icon, text string, face *canvas.FontFace) {
	render.EmitOcticon(buf, x, y+(fieldLineH-iconSize)/2, iconSize, icon, "#959da5")
	render.EmitTextPath(buf, x+iconSize+iconGutter, y+14, text, face)
}

// writeGameList emits a section header followed by one card per game and
// returns the total pixel height consumed. The empty-list case still emits
// the header so the user can confirm the section was wired up.
func writeGameList(buf *bytes.Buffer, title string, games []Game, y int, h2Face, nameFace, fieldFace *canvas.FontFace) int {
	startY := y
	fmt.Fprintf(buf, `<g class="steam-list" data-section="%s" transform="translate(0,%d)">`,
		xmlEscapeAttr(strings.ToLower(strings.ReplaceAll(title, " ", "-"))), y)

	// Section header: upstream uses an inline list-unordered icon here.
	render.EmitOcticon(buf, 0, 0, iconSize, "list-unordered", "#0366d6")
	render.EmitTextPathClass(buf, iconSize+iconGutter, h2BaselineY, title, h2Face, "text-heading")

	rowY := h2BlockH
	for _, g := range games {
		rowY += writeGameCard(buf, g, rowY, nameFace, fieldFace)
	}

	fmt.Fprint(buf, `</g>`)
	return (y - startY) + h2BlockH + (rowY - h2BlockH) + sectionGap
}

// writeGameCard emits one game card: a 32 px icon on the left and a name +
// prose info stack on the right. Returns the consumed vertical pixels so
// the caller can advance to the next card.
func writeGameCard(buf *bytes.Buffer, g Game, y int, nameFace, fieldFace *canvas.FontFace) int {
	// Icon (or placeholder).
	if g.IconB64 != "" {
		fmt.Fprintf(buf,
			`<image x="0" y="%d" width="%d" height="%d" href="%s" clip-path="url(#%s)"><title>%s</title></image>`,
			y, gameIconSize, gameIconSize, xmlEscapeAttr(g.IconB64), gameIconClipID, xmlEscape(g.Name),
		)
	} else {
		fmt.Fprintf(buf,
			`<rect x="0" y="%d" width="%d" height="%d" rx="4" fill="#d0d7de"><title>%s</title></rect>`,
			y, gameIconSize, gameIconSize, xmlEscape(g.Name),
		)
	}

	textX := gameIconSize + 10
	// Game name in the accent colour — upstream uses #58a6ff (text-heading
	// in the classic theme) and a 14px semibold weight.
	render.EmitTextPathClass(buf, textX, y+12, g.Name, nameFace, "text-heading")

	// Info rows: a muted icon+text line per stat. Only stats we have data
	// for are emitted, so the card never leaves dead vertical space. The
	// first (playtime) line is always present; the rest are opportunistic.
	rowH := 18
	infoY := y + 16
	emitInfo := func(icon, text string) {
		render.EmitOcticon(buf, textX, infoY+(rowH-12)/2, 12, icon, "#959da5")
		render.EmitTextPathClass(buf, textX+12+6, infoY+12, text, fieldFace, "text-muted")
		infoY += rowH
	}

	emitInfo("clock", fmt.Sprintf("%.1f hours played", g.PlaytimeHours))
	if g.PercentOfTotal > 0 {
		emitInfo("pulse", formatShare(g.PercentOfTotal))
	}
	if g.LastPlayed != "" {
		emitInfo("calendar", "Last played on "+g.LastPlayed)
	}
	if g.Platform != "" {
		emitInfo("device-desktop", "Mostly on "+g.Platform)
	}
	if g.HasAchievements && g.AchTotal > 0 {
		pct := int(float64(g.AchUnlocked)/float64(g.AchTotal)*100 + 0.5)
		emitInfo("star", fmt.Sprintf("%d / %d achievements (%d%%)", g.AchUnlocked, g.AchTotal, pct))
	}

	// Bottom-pad the card. Total card height is max(icon, text-stack) + 6px.
	cardH := infoY - y + 6
	if cardH < gameIconSize+6 {
		cardH = gameIconSize + 6
	}
	return cardH
}

// formatShare renders a 0..1 playtime share as a percent line, collapsing
// sub-1% values to "<1%" so a tiny slice doesn't display as a misleading 0%.
func formatShare(frac float64) string {
	p := frac * 100
	if p < 1 {
		return "<1% of total playtime"
	}
	return fmt.Sprintf("%.0f%% of total playtime", p)
}

// pluralInt returns "s" when v is anything but exactly 1. Mirrors the
// upstream s(...) helper for English-language pluralisation.
func pluralInt(v int) string {
	if v == 1 {
		return ""
	}
	return "s"
}

// pluralHours returns the noun suffix for a float hour quantity ("hour" vs
// "hours"). Anything other than exactly 1.0 hour gets the plural form;
// this matches upstream's `s(playtime)`.
func pluralHours(v float64) string {
	if v == 1.0 {
		return ""
	}
	return "s"
}

// formatHours formats a playtime quantity as either "N" (for fewer than a
// thousand hours) or "X.YYk" (for >= 1000 hours), matching upstream
// metrics's compact representation that keeps the player summary on one
// line at 12 px.
func formatHours(v float64) string {
	if v >= 1000 {
		return fmt.Sprintf("%.2fk", v/1000.0)
	}
	return fmt.Sprintf("%d", int(v+0.5))
}

// xmlEscape escapes content for use as text inside an SVG element.
func xmlEscape(s string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(s))
	return buf.String()
}

// xmlEscapeAttr escapes content for use as an attribute value. Data: URLs
// can contain '&' and '"' characters that must be escaped for the
// attribute to remain well-formed.
func xmlEscapeAttr(s string) string {
	s = xmlEscape(s)
	s = strings.ReplaceAll(s, `"`, `&quot;`)
	return s
}
