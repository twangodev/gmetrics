package music

type Track struct {
	Name   string
	Artist string
	// Full "data:<mime>;base64,..." URL for an SVG <image href>; empty draws a placeholder.
	ArtworkB64 string
	// Pre-formatted for display; Render does no further formatting.
	PlayedAt string
}

type Data struct {
	Mode     string
	Provider string
	Tracks   []Track
}
