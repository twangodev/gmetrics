package music

// Track is a single rendered row in the music card. PlayedAt is a
// pre-formatted display string ("now playing" or "YYYY-MM-DD HH:MM UTC"),
// not a time.Time, because Render does no further formatting. ArtworkB64
// is a complete "data:<mime>;base64,..." URL ready to drop into an SVG
// <image href=...>; an empty string means "draw a placeholder".
type Track struct {
	Name       string
	Artist     string
	ArtworkB64 string
	PlayedAt   string
}

// Data is the value Fetch returns and Render consumes. Mode and Provider
// are carried through so Render can show them in the header without
// reaching back to Config (matching the pattern used by other plugins).
type Data struct {
	Mode     string
	Provider string
	Tracks   []Track
}
