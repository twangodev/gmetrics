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

const (
	sectionWidth = 440
	sectionGap   = 4

	iconSize    = 16
	rowTextX    = 24
	rowStep     = 18
	headingStep = 30

	metadataHeight = 16
)

// Render emits each enabled section in data.Sections as one SVG fragment.
// Text is converted to glyph paths so the output survives GitHub's Camo
// image proxy, which strips foreignObject and external font references.
func Render(_ *plugin.Env, data Data) (plugin.Fragment, error) {
	if len(data.Sections) == 0 {
		return plugin.Fragment{Width: sectionWidth}, nil
	}

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
	firstDrawn := true
	for i := 0; i < len(data.Sections); i++ {
		sec := data.Sections[i]
		var h int
		var drew bool

		next := ""
		if i+1 < len(data.Sections) {
			next = data.Sections[i+1]
		}
		activityCommunityAdjacent := (sec == "activity" && next == "community") ||
			(sec == "community" && next == "activity")

		if activityCommunityAdjacent {
			h = renderActivityCommunityRow(&body, y, data, ctx)
			drew = true
			i++
		} else {
			h, drew = renderSection(&body, y, sec, data, ctx)
		}

		if !drew {
			continue
		}
		if !firstDrawn {
			y += sectionGap
		}
		y += h
		firstDrawn = false
	}

	height := y
	if height == 0 {
		height = 1
	}

	fragment := fmt.Sprintf(`<g class="plugin-base">%s</g>`, body.String())
	return plugin.Fragment{
		Body:   fragment,
		Width:  sectionWidth,
		Height: height,
	}, nil
}

type sectionContext struct {
	nameFace    *canvas.FontFace
	loginFace   *canvas.FontFace
	headingFace *canvas.FontFace
	rowFace     *canvas.FontFace
	metaFace    *canvas.FontFace
}

func renderSection(w *strings.Builder, y0 int, name string, d Data, ctx sectionContext) (height int, drew bool) {
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
		// Emitted separately by the engine so it lands below every plugin.
		return 0, true
	}
	return 0, false
}

// A non-empty text is rendered verbatim and overrides count/singular/plural.
type statRow struct {
	icon     string
	count    int
	singular string
	plural   string
	text     string
}

func renderHeader(w *strings.Builder, y0 int, d Data, ctx sectionContext) int {
	const (
		avatarSize  = 20
		nameLineY   = 16
		listStartY  = 40
		calStartY   = 30
		calX        = 230
		calCellSize = 11
		calStride   = 15
	)

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

	name := d.User.Name
	if name == "" {
		name = d.User.Login
	}
	if name == "" {
		name = "(unknown)"
	}
	const avatarNameGutter = 6
	nameX := avatarSize + avatarNameGutter
	render.EmitTextPathClass(w, nameX, y0+nameLineY, name, ctx.nameFace, "text-h1")
	if d.User.Login != "" {
		const nameLoginGap = 8
		nameWidth := int(ctx.nameFace.TextWidth(name) + 0.5)
		render.EmitTextPathClass(w, nameX+nameWidth+nameLoginGap, y0+nameLineY, "@"+d.User.Login, ctx.loginFace, "text-muted")
	}

	rowY := y0 + listStartY
	{
		joined := "Joined GitHub"
		if !d.User.CreatedAt.IsZero() {
			years := yearsSince(d.User.CreatedAt, time.Now().UTC())
			joined = fmt.Sprintf("Joined GitHub %d %s ago", years, pluralize(years, "year", "years"))
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

	if len(d.Calendar) > 0 {
		// GitHub's light-mode contribution shades, indexed by intensity level 0..4.
		contributionShades := []string{"#ebedf0", "#9be9a8", "#40c463", "#30a14e", "#216e39"}
		maxCount := 0
		for _, day := range d.Calendar {
			if day.Count > maxCount {
				maxCount = day.Count
			}
		}
		for i, day := range d.Calendar {
			x := calX + i*calStride
			shade := contributionShades[0]
			if day.Count > 0 && maxCount > 0 {
				lvl := 1 + (day.Count-1)*4/maxCount
				if lvl > 4 {
					lvl = 4
				}
				if lvl < 1 {
					lvl = 1
				}
				shade = contributionShades[lvl]
			}
			fmt.Fprintf(w, `<rect x="%d" y="%d" width="%d" height="%d" rx="2" fill="%s"><title>%s: %d</title></rect>`,
				x, y0+calStartY, calCellSize, calCellSize, shade, day.Date, day.Count)
		}
	}

	contributed := fmt.Sprintf("Contributed to %s %s",
		formatCount(d.ContributedTo),
		pluralize(d.ContributedTo, "repository", "repositories"))
	emitIconTextRowAt(w, calX, y0+calStartY+calCellSize+rowStep, "repo-push", contributed, ctx.rowFace)

	if d.Hireable {
		emitHireablePill(w, sectionWidth, y0+4, ctx.rowFace)
	}

	return 76
}

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

func renderRepositories(w *strings.Builder, y0 int, d Data, ctx sectionContext) int {
	heading := fmt.Sprintf("%s %s",
		formatCount(d.Repositories.Count),
		pluralize(d.Repositories.Count, "Repository", "Repositories"))

	diskCount, diskUnit := humanizeDisk(d.Repositories.Disk)

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

	var rightRows []statRow
	if d.Repositories.Stargazers > 0 {
		rightRows = append(rightRows, statRow{icon: "star", count: d.Repositories.Stargazers, singular: "Stargazer", plural: "Stargazers"})
	}
	if d.Repositories.Forks > 0 {
		// Upstream label is "Forker"/"Forkers", not "Fork"/"Forks".
		rightRows = append(rightRows, statRow{icon: "repo-forked", count: d.Repositories.Forks, singular: "Forker", plural: "Forkers"})
	}
	if d.Repositories.Watchers > 0 {
		rightRows = append(rightRows, statRow{icon: "eye", count: d.Repositories.Watchers, singular: "Watcher", plural: "Watchers"})
	}

	return renderTwoColumnStatsSection(w, y0, "repo", heading, leftRows, rightRows, ctx)
}

func renderStatsSection(w *strings.Builder, y0 int, headingIcon, title string, rows []statRow, ctx sectionContext) int {
	return renderStatsSectionWithHeading(w, y0, headingIcon, title, rows, ctx)
}

func renderStatsSectionWithHeading(w *strings.Builder, y0 int, headingIcon, heading string, rows []statRow, ctx sectionContext) int {
	const headingBaselineOffset = 14

	render.EmitOcticon(w, 0, y0, iconSize, headingIcon, "#0366d6")
	render.EmitTextPathClass(w, rowTextX, y0+headingBaselineOffset, heading, ctx.headingFace, "text-heading")

	for i, r := range rows {
		ry := y0 + headingStep + i*rowStep
		render.EmitOcticon(w, 0, ry-iconSize+4, iconSize, r.icon, "#959da5")
		render.EmitTextPath(w, rowTextX, ry, statRowText(r), ctx.rowFace)
	}

	const sectionBottomPadding = 4
	return headingStep + len(rows)*rowStep + sectionBottomPadding
}

// A negative count is a sentinel: render singular alone, with no count prefix.
func statRowText(r statRow) string {
	switch {
	case r.text != "":
		return r.text
	case r.count < 0:
		return r.singular
	default:
		return fmt.Sprintf("%s %s",
			formatCount(r.count),
			pluralize(r.count, r.singular, r.plural))
	}
}

func renderActivityCommunityRow(w *strings.Builder, y0 int, d Data, ctx sectionContext) int {
	const colTwoX = 220

	activityRows := []statRow{
		{icon: "git-commit", count: d.Activity.Commits, singular: "Commit", plural: "Commits"},
		{icon: "checklist", count: d.Activity.PRsReviewed, singular: "Pull request reviewed", plural: "Pull requests reviewed"},
		{icon: "git-pull-request", count: d.Activity.PRsOpened, singular: "Pull request opened", plural: "Pull requests opened"},
		{icon: "issue-opened", count: d.Activity.IssuesOpened, singular: "Issue opened", plural: "Issues opened"},
		{icon: "comment-discussion", count: d.Activity.Comments, singular: "Issue comment", plural: "Issue comments"},
	}
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

	renderHeadingAndRows(w, y0, 0, "pulse", "Activity", activityRows, ctx)
	renderHeadingAndRows(w, y0, colTwoX, "smiley", "Community stats", communityRows, ctx)

	maxRows := len(activityRows)
	if len(communityRows) > maxRows {
		maxRows = len(communityRows)
	}
	const sectionBottomPadding = 4
	return headingStep + maxRows*rowStep + sectionBottomPadding
}

func renderHeadingAndRows(w *strings.Builder, y0, x0 int, headingIcon, heading string, rows []statRow, ctx sectionContext) {
	const headingBaselineOffset = 14
	render.EmitOcticon(w, x0, y0, iconSize, headingIcon, "#0366d6")
	render.EmitTextPathClass(w, x0+rowTextX, y0+headingBaselineOffset, heading, ctx.headingFace, "text-heading")

	for i, r := range rows {
		ry := y0 + headingStep + i*rowStep
		render.EmitOcticon(w, x0, ry-iconSize+4, iconSize, r.icon, "#959da5")
		render.EmitTextPath(w, x0+rowTextX, ry, statRowText(r), ctx.rowFace)
	}
}

func renderTwoColumnStatsSection(w *strings.Builder, y0 int, headingIcon, heading string, leftRows, rightRows []statRow, ctx sectionContext) int {
	const (
		headingBaselineOffset = 14
		colTwoX               = 220
	)

	render.EmitOcticon(w, 0, y0, iconSize, headingIcon, "#0366d6")
	render.EmitTextPathClass(w, rowTextX, y0+headingBaselineOffset, heading, ctx.headingFace, "text-heading")

	drawCol := func(rows []statRow, colX int) {
		for i, r := range rows {
			ry := y0 + headingStep + i*rowStep
			render.EmitOcticon(w, colX, ry-iconSize+4, iconSize, r.icon, "#959da5")
			render.EmitTextPath(w, colX+rowTextX, ry, statRowText(r), ctx.rowFace)
		}
	}
	drawCol(leftRows, 0)
	drawCol(rightRows, colTwoX)

	maxRows := len(leftRows)
	if len(rightRows) > maxRows {
		maxRows = len(rightRows)
	}
	const sectionBottomPadding = 4
	return headingStep + maxRows*rowStep + sectionBottomPadding
}

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

func emitIconTextRow(w *strings.Builder, baselineY int, icon, text string, face *canvas.FontFace) {
	emitIconTextRowAt(w, 0, baselineY, icon, text, face)
}

func emitIconTextRowAt(w *strings.Builder, startX, baselineY int, icon, text string, face *canvas.FontFace) {
	render.EmitOcticon(w, startX, baselineY-iconSize+4, iconSize, icon, "#959da5")
	render.EmitTextPath(w, startX+rowTextX, baselineY, text, face)
}

func renderMetadata(w *strings.Builder, y0 int, d Data, face *canvas.FontFace) {
	msg := "Generated at " + d.Metadata.GeneratedAt + " by twangodev/gmetrics"
	render.EmitTextPathRightAlignedClass(w, sectionWidth-4, y0+12, msg, face, "text-muted")
}

// MetadataFragment renders the footer line as a standalone fragment so the
// engine can place it below every plugin.
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

// yearsSince counts completed years from then to now: a 365-day gap is 1.
func yearsSince(then, now time.Time) int {
	if then.IsZero() || !now.After(then) {
		return 0
	}
	years := now.Year() - then.Year()
	anniversaryNotYetReached := now.YearDay() < then.YearDay()
	if anniversaryNotYetReached {
		years--
	}
	if years < 0 {
		return 0
	}
	return years
}

func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

// humanizeDisk scales a KB disk-usage value into (count, "KB"|"MB"|"GB").
func humanizeDisk(kb int) (int, string) {
	if kb >= 1_000_000 {
		return kb / 1_000_000, "GB"
	}
	if kb >= 1_000 {
		return kb / 1_000, "MB"
	}
	return kb, "KB"
}

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
