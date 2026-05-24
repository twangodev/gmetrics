package languages

// Lang is one aggregated language row produced by Fetch and consumed by
// Render. Bytes is the absolute byte count (post-filter, post-sort). Percent
// is Bytes / Data.Total, in the range [0.0, 1.0].
type Lang struct {
	Name    string
	Color   string
	Bytes   int
	Percent float64
}

// Data is the value Fetch returns and Render consumes. Total is the
// sum of Lang.Bytes across the (post-filter) visible languages.
type Data struct {
	Sections []string
	Details  []string
	Total    int
	Langs    []Lang
	Indepth  bool

	// Indepth-only stats; zero in non-indepth mode.
	IndepthLines   int
	IndepthFiles   int
	IndepthCommits int
}
