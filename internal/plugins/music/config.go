package music

const defaultLastfmURL = "https://ws.audioscrobbler.com/2.0/"

const defaultTrackLimit = 8

type Config struct {
	Provider string
	Mode     string
	User     string
	Token    string
	Limit    int
	URL      string
}

func defaultConfig() Config {
	return Config{
		Provider: "lastfm",
		Mode:     "recent",
		Limit:    defaultTrackLimit,
	}
}
