package steam

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// steamAPIBase is the canonical Steam Web API base. When Config.URL is set
// it replaces this value entirely; the endpoint paths underneath are the
// same in both cases so a single httptest.Server can route by path.
const steamAPIBase = "https://api.steampowered.com"

// playerSummariesResp models the subset of GetPlayerSummaries we render.
type playerSummariesResp struct {
	Response struct {
		Players []struct {
			SteamID     string `json:"steamid"`
			PersonaName string `json:"personaname"`
			AvatarFull  string `json:"avatarfull"`
		} `json:"players"`
	} `json:"response"`
}

// steamLevelResp models the response of GetSteamLevel.
type steamLevelResp struct {
	Response struct {
		PlayerLevel int `json:"player_level"`
	} `json:"response"`
}

// ownedGame is one entry in the GetOwnedGames response. PlaytimeForever is
// in minutes; the conversion to hours happens in fetch.go.
type ownedGame struct {
	AppID           int    `json:"appid"`
	Name            string `json:"name"`
	PlaytimeForever int    `json:"playtime_forever"`
	ImgIconURL      string `json:"img_icon_url"`
}

// ownedGamesResp models the subset of GetOwnedGames we use.
type ownedGamesResp struct {
	Response struct {
		GameCount int         `json:"game_count"`
		Games     []ownedGame `json:"games"`
	} `json:"response"`
}

// recentGame is one entry in the GetRecentlyPlayedGames response. The
// Playtime2Weeks field is in minutes.
type recentGame struct {
	AppID          int    `json:"appid"`
	Name           string `json:"name"`
	Playtime2Weeks int    `json:"playtime_2weeks"`
	ImgIconURL     string `json:"img_icon_url"`
}

// recentGamesResp models the subset of GetRecentlyPlayedGames we use.
type recentGamesResp struct {
	Response struct {
		Games []recentGame `json:"games"`
	} `json:"response"`
}

// apiBase returns the base URL to use for Steam API calls: cfg.URL if it is
// non-empty, otherwise the canonical Steam endpoint. The base never carries
// a trailing slash so callers can concatenate "/ISteamUser/...".
func apiBase(cfg Config) string {
	if cfg.URL != "" {
		return cfg.URL
	}
	return steamAPIBase
}

// doJSON issues a GET against rawURL using hc and decodes the response into
// out. Non-200 responses are reported as an error including the URL path
// (but not the query string, to avoid leaking the API key into log lines).
func doJSON(ctx context.Context, hc *http.Client, rawURL string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// Redact query string from error to avoid leaking token.
		safe := rawURL
		if u, perr := url.Parse(rawURL); perr == nil {
			u.RawQuery = ""
			safe = u.String()
		}
		return fmt.Errorf("GET %s: %s", safe, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode %s: %w", rawURL, err)
	}
	return nil
}

// getPlayerSummary fetches the player's persona name and avatar URL.
func getPlayerSummary(ctx context.Context, hc *http.Client, cfg Config) (playerSummariesResp, error) {
	u := fmt.Sprintf("%s/ISteamUser/GetPlayerSummaries/v2/?key=%s&steamids=%s",
		apiBase(cfg), url.QueryEscape(cfg.Token), url.QueryEscape(cfg.User))
	var out playerSummariesResp
	if err := doJSON(ctx, hc, u, &out); err != nil {
		return out, err
	}
	return out, nil
}

// getLevel fetches the player's Steam community level.
func getLevel(ctx context.Context, hc *http.Client, cfg Config) (steamLevelResp, error) {
	u := fmt.Sprintf("%s/IPlayerService/GetSteamLevel/v1/?key=%s&steamid=%s",
		apiBase(cfg), url.QueryEscape(cfg.Token), url.QueryEscape(cfg.User))
	var out steamLevelResp
	if err := doJSON(ctx, hc, u, &out); err != nil {
		return out, err
	}
	return out, nil
}

// getOwnedGames fetches the user's full owned-games list with appinfo so
// names + icon URLs are present.
func getOwnedGames(ctx context.Context, hc *http.Client, cfg Config) (ownedGamesResp, error) {
	u := fmt.Sprintf("%s/IPlayerService/GetOwnedGames/v1/?key=%s&steamid=%s&include_appinfo=1",
		apiBase(cfg), url.QueryEscape(cfg.Token), url.QueryEscape(cfg.User))
	var out ownedGamesResp
	if err := doJSON(ctx, hc, u, &out); err != nil {
		return out, err
	}
	return out, nil
}

// getRecent fetches the user's recently-played games (last ~2 weeks).
func getRecent(ctx context.Context, hc *http.Client, cfg Config) (recentGamesResp, error) {
	u := fmt.Sprintf("%s/IPlayerService/GetRecentlyPlayedGames/v1/?key=%s&steamid=%s",
		apiBase(cfg), url.QueryEscape(cfg.Token), url.QueryEscape(cfg.User))
	var out recentGamesResp
	if err := doJSON(ctx, hc, u, &out); err != nil {
		return out, err
	}
	return out, nil
}

// iconURL returns the canonical Steam game-icon URL. img_icon_url from the
// API is a bare hash; the JPG sits under media.steampowered.com.
func iconURL(appid int, hash string) string {
	if hash == "" {
		return ""
	}
	return fmt.Sprintf("https://media.steampowered.com/steamcommunity/public/images/apps/%d/%s.jpg", appid, hash)
}
