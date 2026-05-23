package wakatime

// Item is a single row in a WakaTime category list (project, language, editor, OS).
// Percent is normalized into [0, 1] (i.e. the API's percent / 100).
type Item struct {
	Name    string
	Percent float64
	Seconds float64
}

// Data is the trimmed result produced by Fetch and consumed by Render.
type Data struct {
	// Sections is the ordered list of sections to render (e.g. "time",
	// "projects-graphs"). Render copies this from Config so it does not have
	// to know about the original Config value.
	Sections []string
	// TotalHours is total tracked coding time over the range, in hours.
	TotalHours float64
	// DailyAvgHours is the daily-average tracked time, in hours.
	DailyAvgHours float64
	// Days is the lookback used (verbatim for the header label).
	Days int
	// Limit is the per-category cap (preserved from Config for the renderer).
	Limit int

	Projects  []Item
	Languages []Item
	Editors   []Item
	OSes      []Item
}
