package steam

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/twangodev/gmetrics/internal/plugin"
)

// fragmentWidth matches the working width every plugin draws against
// (440 px content inside a 480 px frame, with the engine's standard side
// padding).
const fragmentWidth = 440

// playerHeight is the fixed pixel height of the "player" section: a 48 px
// avatar with 4 px of breathing room top and bottom.
const playerHeight = 56

// listHeaderHeight is the vertical space reserved for a list section's
// title row (e.g. "Most-Played").
const listHeaderHeight = 28

// gameRowHeight is the height of one game row inside a list section.
const gameRowHeight = 40

// listPadBot is extra padding at the bottom of each list section so
// adjacent sections do not visually collide.
const listPadBot = 8

// gameIconSize is the side length of the game icon inside a list row.
const gameIconSize = 32

// avatarSize is the side length of the player avatar.
const avatarSize = 48

// Render lays out each requested section into a single SVG fragment. The
// returned Body is positioned at (0,0); the engine wraps the fragment in a
// translated <g> when composing the final card.
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

	var buf bytes.Buffer
	y := 0
	for _, section := range data.Sections {
		switch section {
		case "player":
			writePlayer(&buf, data.Player, y)
			y += playerHeight
		case "most-played":
			h := writeGameList(&buf, "Most-Played", data.MostPlayed, y)
			y += h
		case "recently-played":
			h := writeGameList(&buf, "Recently-Played", data.Recently, y)
			y += h
		}
	}

	return plugin.Fragment{
		Body:   buf.String(),
		Width:  fragmentWidth,
		Height: y,
	}, nil
}

// writePlayer emits the avatar + persona + level + totals row at the given
// vertical offset.
func writePlayer(buf *bytes.Buffer, p Player, y int) {
	fmt.Fprintf(buf, `<g class="steam-player" transform="translate(0,%d)">`, y)

	// Avatar (or muted placeholder when env.HTTP was unavailable).
	if p.AvatarB64 != "" {
		fmt.Fprintf(buf,
			`<image x="0" y="0" width="%d" height="%d" href="%s"><title>%s</title></image>`,
			avatarSize, avatarSize, xmlEscapeAttr(p.AvatarB64), xmlEscape(p.Name),
		)
	} else {
		fmt.Fprintf(buf,
			`<rect x="0" y="0" width="%d" height="%d" rx="4" fill="var(--color-border)"><title>%s</title></rect>`,
			avatarSize, avatarSize, xmlEscape(p.Name),
		)
	}

	// Name (bold, 14 px) and level + games/hours line (muted, 11 px).
	// Y positions are baselines, chosen so the three lines visually centre
	// against the 48 px avatar.
	textX := avatarSize + 12
	name := p.Name
	if name == "" {
		name = "Unknown"
	}
	fmt.Fprintf(buf,
		`<text x="%d" y="16" font-size="14" font-weight="600" fill="var(--color-text)">%s</text>`,
		textX, xmlEscape(name),
	)
	fmt.Fprintf(buf,
		`<text x="%d" y="32" font-size="12" fill="var(--color-text)">Level %d</text>`,
		textX, p.Level,
	)
	totals := fmt.Sprintf("%d games, %.1f hours", p.TotalGames, p.TotalHours)
	fmt.Fprintf(buf,
		`<text x="%d" y="48" font-size="11" class="text-muted">%s</text>`,
		textX, xmlEscape(totals),
	)

	fmt.Fprint(buf, `</g>`)
}

// writeGameList emits a section header followed by one row per game and
// returns the total pixel height consumed. The empty-list case still emits
// the header so the user can confirm the section was wired up.
func writeGameList(buf *bytes.Buffer, title string, games []Game, y int) int {
	h := listHeaderHeight + len(games)*gameRowHeight + listPadBot
	fmt.Fprintf(buf, `<g class="steam-list" data-section="%s" transform="translate(0,%d)">`,
		xmlEscapeAttr(strings.ToLower(title)), y)

	fmt.Fprintf(buf,
		`<text x="0" y="18" font-size="14" font-weight="600" fill="var(--color-text)">%s</text>`,
		xmlEscape(title),
	)

	for i, g := range games {
		row := listHeaderHeight + i*gameRowHeight
		writeGameRow(buf, g, row)
	}

	fmt.Fprint(buf, `</g>`)
	return h
}

// writeGameRow emits a single game row at the supplied vertical offset
// within its enclosing list <g>.
func writeGameRow(buf *bytes.Buffer, g Game, y int) {
	// Icon (or placeholder).
	if g.IconB64 != "" {
		fmt.Fprintf(buf,
			`<image x="0" y="%d" width="%d" height="%d" href="%s"><title>%s</title></image>`,
			y, gameIconSize, gameIconSize, xmlEscapeAttr(g.IconB64), xmlEscape(g.Name),
		)
	} else {
		fmt.Fprintf(buf,
			`<rect x="0" y="%d" width="%d" height="%d" rx="3" fill="var(--color-border)"><title>%s</title></rect>`,
			y, gameIconSize, gameIconSize, xmlEscape(g.Name),
		)
	}

	textX := gameIconSize + 10
	// Name baseline 14 px below the row's top puts it visually centred
	// against the upper half of the 32 px icon.
	fmt.Fprintf(buf,
		`<text x="%d" y="%d" font-size="12" font-weight="600" fill="var(--color-text)">%s</text>`,
		textX, y+14, xmlEscape(g.Name),
	)
	hours := fmt.Sprintf("%.1f hours", g.PlaytimeHours)
	fmt.Fprintf(buf,
		`<text x="%d" y="%d" font-size="11" class="text-muted">%s</text>`,
		textX, y+28, xmlEscape(hours),
	)
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
