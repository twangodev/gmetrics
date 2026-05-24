package people

// Person is a single rendered avatar tile: a login and the corresponding
// avatar image already inlined as a data: URL. AvatarB64 may be empty when
// the avatar fetch was skipped (e.g. tests with env.HTTP == nil); Render
// treats an empty value as "draw a placeholder".
type Person struct {
	Login     string
	AvatarB64 string
}

// Section is one labelled grid (e.g. "1234 followers") in the rendered card.
// Total is the upstream total reported by GitHub (e.g.
// user.followers.totalCount), which may exceed len(People) when the section
// is limited by Config.Limit.
type Section struct {
	Type   string
	Total  int
	People []Person
}

// Data is the value Fetch returns and Render consumes. Size carries the
// per-cell render size in pixels so Render does not need to look at Config
// again.
type Data struct {
	Sections []Section
	Size     int
}
