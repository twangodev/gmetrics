package languages

type Lang struct {
	Name    string
	Color   string
	Bytes   int
	Percent float64
}

type Data struct {
	Sections []string
	Details  []string
	Total    int
	Langs    []Lang
	Indepth  bool

	// Zero unless Indepth.
	IndepthLines   int
	IndepthFiles   int
	IndepthCommits int
}
