package base

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/tdewolff/canvas"
	"github.com/twangodev/gmetrics/internal/plugin"
	"github.com/twangodev/gmetrics/internal/render"
)

// Layout constants for the base plugin's classic-template-style layout.
//
// The card body is sectionWidth wide. Each row of icon+text starts with a
// 16×16 octicon at x=0 and the accompanying text baseline at x=rowTextX so
// the text clears the icon with a small gutter.
//
// Section heights are computed dynamically (heading + N rows × rowStep)
// rather than hard-coded so adding rows in one place doesn't require
// hunting for a matching constant.
const (
	sectionWidth = 440
	sectionGap   = 4

	iconSize    = 16
	rowTextX    = 24 // octicon at 0..16, text after a small gutter
	rowStep     = 18 // vertical spacing between rows of icon+text
	headingStep = 30 // gap from a section heading baseline to its first row baseline

	metadataHeight = 16
)

// Render lays out every enabled section in data.Sections and returns a
// single SVG fragment ready for the frame composer. Each text string is
// converted to glyph paths so the output is portable through GitHub's Camo
// image proxy (which strips foreignObject and external font references).
//
// Unknown section names (including the legacy "calendar" entry, which is
// now embedded in the header) are silently ignored so config files don't
// have to keep in lock-step with the renderer.
func Render(_ *plugin.Env, data Data) (plugin.Fragment, error) {
	// Font faces: heading text is 16px regular (matching upstream h2), row
	// text is 12px regular, name is 20px bold, login is 14px regular.
	reg12, err := render.Face(12, canvas.FontRegular)
	if err != nil {
		return plugin.Fragment{}, fmt.Errorf("base: load 12px regular face: %w", err)
	}
	reg14, err := render.Face(14, canvas.FontRegular)
	if err != nil {
		return plugin.Fragment{}, fmt.Errorf("base: load 14px regular face: %w", err)
	}
	reg16, err := render.Face(16, canvas.FontRegular)
	if err != nil {
		return plugin.Fragment{}, fmt.Errorf("base: load 16px regular face: %w", err)
	}
	bold20, err := render.Face(20, canvas.FontBold)
	if err != nil {
		return plugin.Fragment{}, fmt.Errorf("base: load 20px bold face: %w", err)
	}
	muted10, err := render.Face(10, canvas.FontRegular)
	if err != nil {
		return plugin.Fragment{}, fmt.Errorf("base: load 10px regular face: %w", err)
	}

	ctx := sectionContext{
		nameFace:    bold20,
		loginFace:   reg14,
		headingFace: reg16,
		rowFace:     reg12,
		metaFace:    muted10,
	}

	var body strings.Builder
	y := 0
	first := true
	// Walk sections, pairing activity+community side-by-side when both are
	// present and adjacent — matches upstream's .largeable two-column flow.
	for i := 0; i < len(data.Sections); i++ {
		sec := data.Sections[i]
		var h int
		var drew bool

		if sec == "activity" && i+1 < len(data.Sections) && data.Sections[i+1] == "community" {
			h = renderActivityCommunityRow(&body, y, data, ctx)
			drew = true
			i++ // also consumed community
		} else if sec == "community" && i+1 < len(data.Sections) && data.Sections[i+1] == "activity" {
			h = renderActivityCommunityRow(&body, y, data, ctx)
			drew = true
			i++
		} else {
			h, drew = renderSection(&body, y, sec, data, ctx)
		}

		if !drew {
			continue
		}
		if !first {
			y += sectionGap
		}
		y += h
		first = false
	}

	height := y
	if height == 0 {
		height = 1 // emit a non-zero box even when every section was skipped
	}

	fragment := fmt.Sprintf(`<g class="plugin-base">%s</g>`, body.String())
	return plugin.Fragment{
		Body:   fragment,
		Width:  sectionWidth,
		Height: height,
	}, nil
}

// sectionContext bundles the font faces shared across all section renderers.
type sectionContext struct {
	nameFace    *canvas.FontFace // 20px bold, used for the user's display name
	loginFace   *canvas.FontFace // 14px regular, used for "@login" in the header
	headingFace *canvas.FontFace // 16px regular, used for h2 section titles
	rowFace     *canvas.FontFace // 12px regular, used for stat rows
	metaFace    *canvas.FontFace // 10px regular, used for the footer
}

// renderSection draws a single named section at the supplied y offset and
// returns (height, drew). When drew is false the renderer did nothing (e.g.
// unknown section name) and y must not advance.
func renderSection(w *strings.Builder, y0 int, name string, d Data, ctx sectionContext) (int, bool) {
	switch name {
	case "header":
		return renderHeader(w, y0, d, ctx), true
	case "activity":
		return renderActivity(w, y0, d, ctx), true
	case "community":
		return renderCommunity(w, y0, d, ctx), true
	case "repositories":
		return renderRepositories(w, y0, d, ctx), true
	case "metadata":
		// Rendered by the engine as a final fragment so it sits below
		// every plugin's output, not inline within the base sections.
		return 0, true
	}
	// "calendar" and any other unknown name: render nothing.
	return 0, false
}

// statRow is one "<icon> <count> <label>" line. count is comma-formatted
// and the label is pluralized via the singular/plural pair.
//
// When text is non-empty it is rendered verbatim and the count/singular/
// plural fields are ignored. This lets callers compose verb-prefixed lines
// like "Member of 6 organizations" without bolting prefix-handling onto
// the formatter.
type statRow struct {
	icon string
	// count + singular/plural are used when text == ""
	count    int
	singular string
	plural   string
	// text, when non-empty, is rendered as-is and overrides count/singular/plural.
	text string
}

// renderHeader draws the top-of-card identity block. Layout:
//
//	[avatar 20x20] [Name 20px bold blue]   [@login 14px muted]
//	[cake icon]  Joined GitHub N year(s) ago         [14-day]
//	[people]     Followed by N user(s)               [calendar]
//	[briefcase]  Available for hire                  [strip   ]
//
// The calendar mini-strip lives in a right column starting at calX.
func renderHeader(w *strings.Builder, y0 int, d Data, ctx sectionContext) int {
	const (
		avatarSize  = 20
		nameLineY   = 16 // baseline for the name row (relative to y0)
		listStartY  = 40 // baseline for the first row of the left list
		calStartY   = 30 // top of the calendar strip (relative to y0)
		calX        = 230
		calCellSize = 11
		calStride   = 15
	)

	// Avatar circle clip. Avatar drawn at (0, y0+nameLineY-avatarSize+4) so
	// it sits visually centered with the 20px name baseline (cap height ~14).
	avatarY := y0 + nameLineY - avatarSize + 4
	cx := avatarSize / 2
	cy := avatarY + avatarSize/2
	if d.AvatarB64 != "" {
		fmt.Fprintf(w, `<defs><clipPath id="avatar-clip"><circle cx="%d" cy="%d" r="%d"/></clipPath></defs>`,
			cx, cy, avatarSize/2)
		fmt.Fprintf(w, `<image x="0" y="%d" width="%d" height="%d" href="%s" clip-path="url(#avatar-clip)" preserveAspectRatio="xMidYMid slice"/>`,
			avatarY, avatarSize, avatarSize, d.AvatarB64)
	} else {
		fmt.Fprintf(w, `<circle cx="%d" cy="%d" r="%d" fill="#d0d7de"/>`, cx, cy, avatarSize/2)
	}

	// Name and login on the same line, separated by an 8px gap. The name
	// uses text-h1 (20px bold blue), login uses text-muted (14px).
	name := d.User.Name
	if name == "" {
		name = d.User.Login
	}
	if name == "" {
		name = "(unknown)"
	}
	nameX := avatarSize + 6 // 6px gutter between avatar and name
	render.EmitTextPathClass(w, nameX, y0+nameLineY, name, ctx.nameFace, "text-h1")
	if d.User.Login != "" {
		nameWidth := int(ctx.nameFace.TextWidth(name) + 0.5)
		render.EmitTextPathClass(w, nameX+nameWidth+8, y0+nameLineY, "@"+d.User.Login, ctx.loginFace, "text-muted")
	}

	// Left column: cake (Joined GitHub …), people (Followed by …), and
	// (optionally) briefcase (Available for hire). Each row is icon at
	// (0, iconY) with the text baseline at (rowTextX, baseline).
	rowY := y0 + listStartY
	{
		// Joined GitHub N year(s) ago. Compute N from CreatedAt; fall back
		// to a generic phrasing when the timestamp is the zero value (e.g.
		// in unit tests that don't populate it).
		var joined string
		if !d.User.CreatedAt.IsZero() {
			years := yearsSince(d.User.CreatedAt, time.Now().UTC())
			joined = fmt.Sprintf("Joined GitHub %d %s ago", years, pluralize(years, "year", "years"))
		} else {
			joined = "Joined GitHub"
		}
		emitIconTextRow(w, rowY, "cake", joined, ctx.rowFace)
	}
	rowY += rowStep
	{
		followed := fmt.Sprintf("Followed by %s %s",
			formatCount(d.User.Followers),
			pluralize(d.User.Followers, "user", "users"))
		emitIconTextRow(w, rowY, "people", followed, ctx.rowFace)
	}
	// "Available for hire" is rendered as a small outlined pill in the
	// top-right of the card (drawn at end of renderHeader so it overlays
	// the right side of the name row). Skipped when not hireable.

	// Right column: 14-day contribution mini-strip. Each cell is 11×11 at
	// a 15px stride. Use upstream's light-mode shade palette and bucket
	// counts into [0,1..4] by max-count.
	if len(d.Calendar) > 0 {
		shades := []string{"#ebedf0", "#9be9a8", "#40c463", "#30a14e", "#216e39"}
		maxCount := 0
		for _, day := range d.Calendar {
			if day.Count > maxCount {
				maxCount = day.Count
			}
		}
		for i, day := range d.Calendar {
			x := calX + i*calStride
			shade := shades[0]
			if day.Count > 0 && maxCount > 0 {
				lvl := 1 + (day.Count-1)*4/maxCount
				if lvl > 4 {
					lvl = 4
				}
				if lvl < 1 {
					lvl = 1
				}
				shade = shades[lvl]
			}
			fmt.Fprintf(w, `<rect x="%d" y="%d" width="%d" height="%d" rx="2" fill="%s"><title>%s: %d</title></rect>`,
				x, y0+calStartY, calCellSize, calCellSize, shade, day.Date, day.Count)
		}
	}

	// "Contributed to N repositories" sits under the calendar strip,
	// aligned to calX so it reads as part of the right column.
	contributed := fmt.Sprintf("Contributed to %s %s",
		formatCount(d.ContributedTo),
		pluralize(d.ContributedTo, "repository", "repositories"))
	emitIconTextRowAt(w, calX, y0+calStartY+calCellSize+rowStep, "repo-push", contributed, ctx.rowFace)

	if d.Hireable {
		emitHireablePill(w, sectionWidth, y0+4, ctx.rowFace)
	}

	return 76
}

// renderActivity draws the "Activity" section. Row phrasing matches
// upstream lowlighter/metrics' classic template
// (source/templates/classic/partials/base.activity+community.ejs): note
// that the labels are capitalized ("Commits", "Pull request reviewed", …).
func renderActivity(w *strings.Builder, y0 int, d Data, ctx sectionContext) int {
	rows := []statRow{
		{icon: "git-commit", count: d.Activity.Commits, singular: "Commit", plural: "Commits"},
		{icon: "checklist", count: d.Activity.PRsReviewed, singular: "Pull request reviewed", plural: "Pull requests reviewed"},
		{icon: "git-pull-request", count: d.Activity.PRsOpened, singular: "Pull request opened", plural: "Pull requests opened"},
		{icon: "issue-opened", count: d.Activity.IssuesOpened, singular: "Issue opened", plural: "Issues opened"},
		{icon: "comment-discussion", count: d.Activity.Comments, singular: "Issue comment", plural: "Issue comments"},
	}
	return renderStatsSection(w, y0, "pulse", "Activity", rows, ctx)
}

// renderCommunity draws the "Community stats" section. Upstream uses
// verb-prefixed phrasings ("Member of N organizations", "Following N
// users", "Sponsoring N repositories", "Starred N repositories",
// "Watching N repositories"), so each row supplies a fully formatted
// text string via statRow.text rather than relying on the default
// "<count> <label>" formatter.
func renderCommunity(w *strings.Builder, y0 int, d Data, ctx sectionContext) int {
	rows := []statRow{
		{icon: "organization", text: fmt.Sprintf("Member of %s %s",
			formatCount(d.Community.Orgs),
			pluralize(d.Community.Orgs, "organization", "organizations"))},
		{icon: "people", text: fmt.Sprintf("Following %s %s",
			formatCount(d.Community.Following),
			pluralize(d.Community.Following, "user", "users"))},
		{icon: "heart", text: fmt.Sprintf("Sponsoring %s %s",
			formatCount(d.Community.Sponsors),
			pluralize(d.Community.Sponsors, "repository", "repositories"))},
		{icon: "star", text: fmt.Sprintf("Starred %s %s",
			formatCount(d.Community.Stars),
			pluralize(d.Community.Stars, "repository", "repositories"))},
		{icon: "eye", text: fmt.Sprintf("Watching %s %s",
			formatCount(d.Community.Watching),
			pluralize(d.Community.Watching, "repository", "repositories"))},
	}
	return renderStatsSection(w, y0, "smiley", "Community stats", rows, ctx)
}

// renderRepositories draws the "Repositories" section. Optional rows
// (license, releases, packages, forks, stargazers, watchers) are emitted
// only when their underlying data is non-zero, since several of those
// fields are stretch-goal-unpopulated in v1.
//
// Layout mirrors upstream lowlighter/metrics' .largeable two-column flow
// inside the repositories card (base.repositories.ejs): the rows are split
// across a left and a right column rather than stacked vertically. The
// left column collects license/releases/packages/disk; the right collects
// stargazers/forkers/watchers (with sponsors when present).
func renderRepositories(w *strings.Builder, y0 int, d Data, ctx sectionContext) int {
	heading := fmt.Sprintf("%s %s",
		formatCount(d.Repositories.Count),
		pluralize(d.Repositories.Count, "Repository", "Repositories"))

	// Disk usage: GitHub returns KB. Format as GB / MB / KB based on size,
	// matching upstream's "12 GB used" phrasing.
	diskCount, diskUnit := humanizeDisk(d.Repositories.Disk)

	// Left column: license, releases, packages, disk. Upstream orders
	// license first, then releases, then packages, then disk usage; we
	// follow that ordering even though only some of these are populated.
	var leftRows []statRow
	if strings.TrimSpace(d.Repositories.License) != "" {
		leftRows = append(leftRows, statRow{
			icon: "law",
			text: fmt.Sprintf("Prefers %s license", d.Repositories.License),
		})
	}
	if d.Repositories.Releases > 0 {
		leftRows = append(leftRows, statRow{icon: "tag", count: d.Repositories.Releases, singular: "Release", plural: "Releases"})
	}
	if d.Repositories.Packages > 0 {
		leftRows = append(leftRows, statRow{icon: "package", count: d.Repositories.Packages, singular: "Package", plural: "Packages"})
	}
	leftRows = append(leftRows, statRow{
		icon: "database",
		text: fmt.Sprintf("%d %s used", diskCount, diskUnit),
	})

	// Right column: stargazers, forkers, watchers.
	var rightRows []statRow
	if d.Repositories.Stargazers > 0 {
		rightRows = append(rightRows, statRow{icon: "star", count: d.Repositories.Stargazers, singular: "Stargazer", plural: "Stargazers"})
	}
	if d.Repositories.Forks > 0 {
		// Upstream label is "Forker" (singular) / "Forkers" (plural),
		// not "Fork"/"Forks". Match it exactly.
		rightRows = append(rightRows, statRow{icon: "repo-forked", count: d.Repositories.Forks, singular: "Forker", plural: "Forkers"})
	}
	if d.Repositories.Watchers > 0 {
		rightRows = append(rightRows, statRow{icon: "eye", count: d.Repositories.Watchers, singular: "Watcher", plural: "Watchers"})
	}

	return renderTwoColumnStatsSection(w, y0, "repo", heading, leftRows, rightRows, ctx)
}

// renderStatsSection draws a section heading using a generic title (e.g.
// "Activity") and the supplied stat rows underneath.
func renderStatsSection(w *strings.Builder, y0 int, headingIcon, title string, rows []statRow, ctx sectionContext) int {
	return renderStatsSectionWithHeading(w, y0, headingIcon, title, rows, ctx)
}

// renderStatsSectionWithHeading is the inner implementation. The heading is
// rendered with the section's octicon at x=0 and the text starting at
// rowTextX. Rows are emitted at headingStep + i*rowStep from y0.
func renderStatsSectionWithHeading(w *strings.Builder, y0 int, headingIcon, heading string, rows []statRow, ctx sectionContext) int {
	const headingBaselineOffset = 14

	// Heading: 16x16 octicon at (0, y0), text at (rowTextX, baseline).
	render.EmitOcticon(w, 0, y0, iconSize, headingIcon, "#0366d6")
	render.EmitTextPathClass(w, rowTextX, y0+headingBaselineOffset, heading, ctx.headingFace, "text-heading")

	// Rows.
	for i, r := range rows {
		ry := y0 + headingStep + i*rowStep
		render.EmitOcticon(w, 0, ry-iconSize+4, iconSize, r.icon, "#959da5")

		var text string
		switch {
		case r.text != "":
			// Caller pre-formatted the row (verb-prefixed phrasings, license
			// preference, "N GB used", …). Render verbatim.
			text = r.text
		case r.count < 0:
			// Sentinel: render the label as-is without a count prefix.
			text = r.singular
		default:
			text = fmt.Sprintf("%s %s",
				formatCount(r.count),
				pluralize(r.count, r.singular, r.plural))
		}
		render.EmitTextPath(w, rowTextX, ry, text, ctx.rowFace)
	}

	return headingStep + len(rows)*rowStep + 4 // small padding below the last row
}

// renderActivityCommunityRow draws Activity in the left column and
// Community stats in the right column, side-by-side. Matches upstream's
// .largeable two-column layout (base.activity+community.ejs wraps both
// sections in a single <section class="largeable"><div class="row">…).
func renderActivityCommunityRow(w *strings.Builder, y0 int, d Data, ctx sectionContext) int {
	const colTwoX = 220

	// Build activity rows (left).
	activityRows := []statRow{
		{icon: "git-commit", count: d.Activity.Commits, singular: "Commit", plural: "Commits"},
		{icon: "checklist", count: d.Activity.PRsReviewed, singular: "Pull request reviewed", plural: "Pull requests reviewed"},
		{icon: "git-pull-request", count: d.Activity.PRsOpened, singular: "Pull request opened", plural: "Pull requests opened"},
		{icon: "issue-opened", count: d.Activity.IssuesOpened, singular: "Issue opened", plural: "Issues opened"},
		{icon: "comment-discussion", count: d.Activity.Comments, singular: "Issue comment", plural: "Issue comments"},
	}
	// Build community rows (right) with verb-prefix phrasing.
	communityRows := []statRow{
		{icon: "organization", text: fmt.Sprintf("Member of %s %s",
			formatCount(d.Community.Orgs),
			pluralize(d.Community.Orgs, "organization", "organizations"))},
		{icon: "people", text: fmt.Sprintf("Following %s %s",
			formatCount(d.Community.Following),
			pluralize(d.Community.Following, "user", "users"))},
		{icon: "heart", text: fmt.Sprintf("Sponsoring %s %s",
			formatCount(d.Community.Sponsors),
			pluralize(d.Community.Sponsors, "repository", "repositories"))},
		{icon: "star", text: fmt.Sprintf("Starred %s %s",
			formatCount(d.Community.Stars),
			pluralize(d.Community.Stars, "repository", "repositories"))},
		{icon: "eye", text: fmt.Sprintf("Watching %s %s",
			formatCount(d.Community.Watching),
			pluralize(d.Community.Watching, "repository", "repositories"))},
	}

	// Activity heading + rows in left column (x = 0..colTwoX).
	renderHeadingAndRows(w, y0, 0, "pulse", "Activity", activityRows, ctx)
	// Community heading + rows in right column (x = colTwoX..sectionWidth).
	renderHeadingAndRows(w, y0, colTwoX, "smiley", "Community stats", communityRows, ctx)

	maxRows := len(activityRows)
	if len(communityRows) > maxRows {
		maxRows = len(communityRows)
	}
	return headingStep + maxRows*rowStep + 4
}

// renderHeadingAndRows draws a heading + rows at a given (x, y) origin.
// Used by renderActivityCommunityRow for the side-by-side layout.
func renderHeadingAndRows(w *strings.Builder, y0, x0 int, headingIcon, heading string, rows []statRow, ctx sectionContext) {
	const headingBaselineOffset = 14
	render.EmitOcticon(w, x0, y0, iconSize, headingIcon, "#0366d6")
	render.EmitTextPathClass(w, x0+rowTextX, y0+headingBaselineOffset, heading, ctx.headingFace, "text-heading")

	for i, r := range rows {
		ry := y0 + headingStep + i*rowStep
		render.EmitOcticon(w, x0, ry-iconSize+4, iconSize, r.icon, "#959da5")
		var text string
		switch {
		case r.text != "":
			text = r.text
		case r.count < 0:
			text = r.singular
		default:
			text = fmt.Sprintf("%s %s",
				formatCount(r.count),
				pluralize(r.count, r.singular, r.plural))
		}
		render.EmitTextPath(w, x0+rowTextX, ry, text, ctx.rowFace)
	}
}

// renderTwoColumnStatsSection draws a section heading with the supplied
// rows split across two columns. The left column starts at x=0; the right
// starts at x=colTwoX. Both columns step y in parallel using rowStep, so
// the column with more rows determines the section's overall height.
//
// This mirrors upstream lowlighter/metrics' .largeable two-column flow
// inside the repositories card (see base.repositories.ejs).
func renderTwoColumnStatsSection(w *strings.Builder, y0 int, headingIcon, heading string, leftRows, rightRows []statRow, ctx sectionContext) int {
	const (
		headingBaselineOffset = 14
		colTwoX               = 220 // ~half of sectionWidth (440) with a ~10px gap
	)

	// Heading.
	render.EmitOcticon(w, 0, y0, iconSize, headingIcon, "#0366d6")
	render.EmitTextPathClass(w, rowTextX, y0+headingBaselineOffset, heading, ctx.headingFace, "text-heading")

	// Helper to draw a single row at the supplied column x-offset and row index.
	drawCol := func(rows []statRow, colX int) {
		for i, r := range rows {
			ry := y0 + headingStep + i*rowStep
			render.EmitOcticon(w, colX, ry-iconSize+4, iconSize, r.icon, "#959da5")

			var text string
			switch {
			case r.text != "":
				text = r.text
			case r.count < 0:
				text = r.singular
			default:
				text = fmt.Sprintf("%s %s",
					formatCount(r.count),
					pluralize(r.count, r.singular, r.plural))
			}
			render.EmitTextPath(w, colX+rowTextX, ry, text, ctx.rowFace)
		}
	}
	drawCol(leftRows, 0)
	drawCol(rightRows, colTwoX)

	maxRows := len(leftRows)
	if len(rightRows) > maxRows {
		maxRows = len(rightRows)
	}
	return headingStep + maxRows*rowStep + 4
}

// emitHireablePill draws a hollow green "Hireable" pill anchored to
// the right edge at rightX.
func emitHireablePill(w *strings.Builder, rightX, topY int, face *canvas.FontFace) {
	const (
		text   = "Hireable"
		padX   = 8
		padY   = 4
		pillH  = 18
		accent = "#1a7f37"
	)
	pillW := int(face.TextWidth(text)+0.5) + 2*padX
	pillX := rightX - pillW
	fmt.Fprintf(w,
		`<rect x="%d" y="%d" width="%d" height="%d" rx="%d" fill="none" stroke="%s" stroke-width="1"/>`,
		pillX, topY, pillW, pillH, pillH/2, accent,
	)
	render.EmitTextPathColor(w, pillX+padX, topY+pillH-padY-1, text, face, accent)
}

// emitIconTextRow draws an octicon + text at the supplied baseline.
func emitIconTextRow(w *strings.Builder, baselineY int, icon, text string, face *canvas.FontFace) {
	emitIconTextRowAt(w, 0, baselineY, icon, text, face)
}

// emitIconTextRowAt is like emitIconTextRow but anchored at startX.
func emitIconTextRowAt(w *strings.Builder, startX, baselineY int, icon, text string, face *canvas.FontFace) {
	render.EmitOcticon(w, startX, baselineY-iconSize+4, iconSize, icon, "#959da5")
	render.EmitTextPath(w, startX+rowTextX, baselineY, text, face)
}

func renderMetadata(w *strings.Builder, y0 int, d Data, face *canvas.FontFace) {
	msg := "Generated at " + d.Metadata.GeneratedAt + " by twangodev/gmetrics"
	render.EmitTextPathRightAlignedClass(w, sectionWidth-4, y0+12, msg, face, "text-muted")
}

// MetadataFragment renders the footer line as a standalone fragment so
// the engine can place it below every plugin.
func MetadataFragment(d any) (plugin.Fragment, error) {
	bd, ok := d.(Data)
	if !ok {
		return plugin.Fragment{}, nil
	}
	face, err := render.Face(10, canvas.FontRegular)
	if err != nil {
		return plugin.Fragment{}, fmt.Errorf("metadata: load face: %w", err)
	}
	var w strings.Builder
	renderMetadata(&w, 0, bd, face)
	return plugin.Fragment{
		Body:   w.String(),
		Width:  sectionWidth,
		Height: metadataHeight,
	}, nil
}

// yearsSince returns the integer number of completed years between then
// and now. A 364-day gap returns 0; a 365-day gap returns 1. Negative or
// future timestamps return 0.
func yearsSince(then, now time.Time) int {
	if then.IsZero() || !now.After(then) {
		return 0
	}
	years := now.Year() - then.Year()
	// Subtract one year if the anniversary hasn't passed this year yet.
	if now.YearDay() < then.YearDay() {
		years--
	}
	if years < 0 {
		return 0
	}
	return years
}

// pluralize picks the singular form when n == 1, plural otherwise. The two
// forms are passed explicitly so callers can handle English idiosyncrasies
// (repository → repositories) without us bundling a pluralization table.
func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

// humanizeDisk picks the most appropriate unit for a disk-usage value
// expressed in KB. Returns (count, unit) where count is the count to show
// (already scaled) and unit is "KB", "MB", or "GB". Above 1 GB we round
// to one decimal place by multiplying by 10 internally; below 1 GB we
// just truncate to integer KB/MB.
func humanizeDisk(kb int) (int, string) {
	if kb >= 1_000_000 {
		// Convert to tenths of a GB so the displayed integer reads as "12"
		// for a 12 GB user; for tenths, we'd need a float renderer. For
		// now show integer GB to match upstream's "12 GB" style.
		return kb / 1_000_000, "GB"
	}
	if kb >= 1_000 {
		return kb / 1_000, "MB"
	}
	return kb, "KB"
}

// formatCount renders an int with thousands separators (", ").
func formatCount(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	s := strconv.Itoa(n)
	var b strings.Builder
	for i, r := range s {
		if i != 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}
