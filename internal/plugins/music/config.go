package music

// defaultLastfmURL is the public Last.fm 2.0 endpoint. Tests override this
// via Config.URL so the plugin can be exercised against an httptest.Server
// without flipping a package-level variable.
const defaultLastfmURL = "https://ws.audioscrobbler.com/2.0/"

// Config is the typed configuration for the music plugin. Only the
// `provider: lastfm` + `mode: recent` combination is supported in v1 (see
// docs/superpowers/specs/2026-05-22-gmetrics-port-design.md §13).
type Config struct {
	// Provider must be "lastfm" in v1. Any other value causes Fetch to
	// return an error mentioning lastfm; this is the v1 scope hatch.
	Provider string
	// Mode selects which Last.fm endpoint to call. Only "recent" is
	// implemented; passing other modes is reserved for future work.
	Mode string
	// User is the Last.fm username whose tracks should be fetched.
	User string
	// Token is the Last.fm API key (see https://www.last.fm/api/account/create).
	Token string
	// Limit caps the number of returned tracks. Last.fm allows up to 200;
	// gmetrics typically uses 4 to keep the card compact.
	Limit int
	// URL is an optional override of the Last.fm API base URL. When empty
	// defaultLastfmURL is used. The field exists so tests can swap in a
	// httptest.Server URL without touching package-level state.
	URL string
}

// defaultConfig returns a Config with the user's documented defaults: the
// Last.fm provider, the "recent" mode, and a 4-track limit. All other
// fields are zero-valued and must be supplied by the user.
func defaultConfig() Config {
	return Config{
		Provider: "lastfm",
		Mode:     "recent",
		Limit:    4,
	}
}
