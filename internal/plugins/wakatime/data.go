package wakatime

type Item struct {
	Name string
	// Percent is normalized into [0, 1] (the API's percent / 100).
	Percent float64
	Seconds float64
}

type Data struct {
	Sections      []string
	TotalHours    float64
	DailyAvgHours float64
	Days          int
	Limit         int

	Projects  []Item
	Languages []Item
	Editors   []Item
	OSes      []Item
}
