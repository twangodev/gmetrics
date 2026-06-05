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
	AppID   int
	Name    string
	IconB64 string
	// PlaytimeHours is lifetime playtime for MostPlayed and last-two-weeks for Recently, from the minutes Steam reports.
	PlaytimeHours  float64
	LastPlayed     string
	PercentOfTotal float64
	Platform       string
	// AchUnlocked and AchTotal are meaningful only when HasAchievements is true.
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
