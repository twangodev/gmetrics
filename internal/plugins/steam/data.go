package steam

// Player summarises the values rendered in the "player" section of the
// steam card. AvatarB64 is a "data:<mime>;base64,..." URL when the avatar
// fetch succeeded and empty otherwise (Render falls back to a placeholder).
type Player struct {
	Name       string
	AvatarB64  string
	Level      int
	TotalGames int
	TotalHours float64
}

// Game describes a single row in either the most-played or recently-played
// section. PlaytimeHours is total playtime for the most-played list and
// last-two-weeks playtime for the recently-played list; both arrive in
// minutes from the Steam API and are converted on ingest. LastPlayed is
// reserved for a future per-game last-played string; v1 leaves it empty
// because Steam's REST endpoints we use don't return that value directly.
type Game struct {
	AppID         int
	Name          string
	IconB64       string
	PlaytimeHours float64
	LastPlayed    string
}

// Data is the value Fetch returns and Render consumes. Sections is carried
// through verbatim from the Config so Render does not need to look at Config
// again. MostPlayed and Recently are populated independently; the slice for
// a section the user did not enable may still be filled (it is simply
// ignored by Render).
type Data struct {
	Sections   []string
	Player     Player
	MostPlayed []Game
	Recently   []Game
}
