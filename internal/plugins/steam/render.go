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

// fragmentWidth is 440 px of content inside the engine's 480 px frame; it
// must match the other plugins so stacked cards align.
const fragmentWidth = 440

const gameIconSize = 32

const (
	gameIconCornerFrac = 0.125
	gameIconClipID     = "steam-icon-round"
)

const (
	iconSize   = 16
	iconGutter = 8

	h2BaselineY = 16
	h2BlockH    = 24
	fieldLineH  = 20
	sectionGap  = 8
)

func (*Plugin) Render(env *plugin.Env, raw any) (plugin.Fragment, error) {
	data, ok := raw.(Data)
	if !ok {
		return plugin.Fragment{}, fmt.Errorf("steam: render: want Data, got %T", raw)
	}
	if len(data.Sections) == 0 {
		data.Sections = []string{"player", "most-played", "recently-played"}
	}

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

	render.EmitRoundedClip(&buf, gameIconClipID, gameIconCornerFrac)

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

func writePlayer(buf *bytes.Buffer, p Player, y int, fieldFace *canvas.FontFace) int {
	fmt.Fprintf(buf, `<g class="steam-player" transform="translate(0,%d)">`, y)

	name := p.Name
	if name == "" {
		name = "Unknown"
	}

	const rightColumnX = fragmentWidth / 2
	writeField(buf, 0, 0, "person", name, fieldFace)
	writeField(buf, 0, fieldLineH, "package", fmt.Sprintf("%d game%s", p.TotalGames, pluralInt(p.TotalGames)), fieldFace)
	writeField(buf, rightColumnX, 0, "star", fmt.Sprintf("Steam level %d", p.Level), fieldFace)
	writeField(buf, rightColumnX, fieldLineH, "clock", fmt.Sprintf("%s hour%s played", formatHours(p.TotalHours), pluralHours(p.TotalHours)), fieldFace)

	fmt.Fprint(buf, `</g>`)
	return 2*fieldLineH + sectionGap
}

func writeField(buf *bytes.Buffer, x, y int, icon, text string, face *canvas.FontFace) {
	render.EmitOcticon(buf, x, y+(fieldLineH-iconSize)/2, iconSize, icon, "#959da5")
	render.EmitTextPath(buf, x+iconSize+iconGutter, y+14, text, face)
}

func writeGameList(buf *bytes.Buffer, title string, games []Game, y int, h2Face, nameFace, fieldFace *canvas.FontFace) int {
	startY := y
	fmt.Fprintf(buf, `<g class="steam-list" data-section="%s" transform="translate(0,%d)">`,
		xmlEscapeAttr(strings.ToLower(strings.ReplaceAll(title, " ", "-"))), y)

	render.EmitOcticon(buf, 0, 0, iconSize, "list-unordered", "#0366d6")
	render.EmitTextPathClass(buf, iconSize+iconGutter, h2BaselineY, title, h2Face, "text-heading")

	rowY := h2BlockH
	for _, g := range games {
		rowY += writeGameCard(buf, g, rowY, nameFace, fieldFace)
	}

	fmt.Fprint(buf, `</g>`)
	return (y - startY) + h2BlockH + (rowY - h2BlockH) + sectionGap
}

func writeGameCard(buf *bytes.Buffer, g Game, y int, nameFace, fieldFace *canvas.FontFace) int {
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

	const iconTextGap = 10
	textX := gameIconSize + iconTextGap
	render.EmitTextPathClass(buf, textX, y+12, g.Name, nameFace, "text-heading")

	const (
		infoIconSize   = 12
		infoIconGutter = 6
		infoRowH       = 18
	)
	infoY := y + 16
	emitInfo := func(icon, text string) {
		render.EmitOcticon(buf, textX, infoY+(infoRowH-infoIconSize)/2, infoIconSize, icon, "#959da5")
		render.EmitTextPathClass(buf, textX+infoIconSize+infoIconGutter, infoY+12, text, fieldFace, "text-muted")
		infoY += infoRowH
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

	const cardBottomPad = 6
	cardH := infoY - y + cardBottomPad
	if cardH < gameIconSize+cardBottomPad {
		cardH = gameIconSize + cardBottomPad
	}
	return cardH
}

func formatShare(frac float64) string {
	p := frac * 100
	if p < 1 {
		return "<1% of total playtime"
	}
	return fmt.Sprintf("%.0f%% of total playtime", p)
}

func pluralInt(v int) string {
	if v == 1 {
		return ""
	}
	return "s"
}

func pluralHours(v float64) string {
	if v == 1.0 {
		return ""
	}
	return "s"
}

func formatHours(v float64) string {
	if v >= 1000 {
		return fmt.Sprintf("%.2fk", v/1000.0)
	}
	return fmt.Sprintf("%d", int(v+0.5))
}

func xmlEscape(s string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(s))
	return buf.String()
}

// xml.EscapeText leaves '"' intact, so escape it for attribute contexts.
func xmlEscapeAttr(s string) string {
	s = xmlEscape(s)
	s = strings.ReplaceAll(s, `"`, `&quot;`)
	return s
}
