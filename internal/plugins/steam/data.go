package steam

type Player struct {
	Name string
	// AvatarB64 is a "data:<mime>;base64,..." URL, or "" when the fetch failed.
	AvatarB64  string
	Level      int
	TotalGames int
	TotalHours float64
}

type Game struct {
	AppID           int
	Name            string
	IconB64         string
	LifetimeHours   float64
	LastPlayed      string
	PercentOfTotal  float64
	Platform        string
	HasAchievements bool
	AchUnlocked     int
	AchTotal        int
}

type Data struct {
	Sections   []string
	Player     Player
	MostPlayed []Game
	Recently   []Game
}
