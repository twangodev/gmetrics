package steam

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

const steamAPIBase = "https://api.steampowered.com"

type playerSummariesResp struct {
	Response struct {
		Players []struct {
			SteamID     string `json:"steamid"`
			PersonaName string `json:"personaname"`
			AvatarFull  string `json:"avatarfull"`
		} `json:"players"`
	} `json:"response"`
}

type steamLevelResp struct {
	Response struct {
		PlayerLevel int `json:"player_level"`
	} `json:"response"`
}

// Playtime fields are in minutes.
type ownedGame struct {
	AppID           int    `json:"appid"`
	Name            string `json:"name"`
	PlaytimeForever int    `json:"playtime_forever"`
	Playtime2Weeks  int    `json:"playtime_2weeks"`
	ImgIconURL      string `json:"img_icon_url"`
	RtimeLastPlayed int64  `json:"rtime_last_played"`
	PlaytimeWindows int    `json:"playtime_windows_forever"`
	PlaytimeMac     int    `json:"playtime_mac_forever"`
	PlaytimeLinux   int    `json:"playtime_linux_forever"`
	PlaytimeDeck    int    `json:"playtime_deck_forever"`
}

type ownedGamesResp struct {
	Response struct {
		GameCount int         `json:"game_count"`
		Games     []ownedGame `json:"games"`
	} `json:"response"`
}

// Playtime fields are in minutes; this endpoint omits rtime_last_played, so
// fetch.go joins against owned games by appid to recover a last-played date.
type recentGame struct {
	AppID           int    `json:"appid"`
	Name            string `json:"name"`
	Playtime2Weeks  int    `json:"playtime_2weeks"`
	PlaytimeForever int    `json:"playtime_forever"`
	ImgIconURL      string `json:"img_icon_url"`
	PlaytimeWindows int    `json:"playtime_windows_forever"`
	PlaytimeMac     int    `json:"playtime_mac_forever"`
	PlaytimeLinux   int    `json:"playtime_linux_forever"`
	PlaytimeDeck    int    `json:"playtime_deck_forever"`
}

type recentGamesResp struct {
	Response struct {
		Games []recentGame `json:"games"`
	} `json:"response"`
}

func apiBase(cfg Config) string {
	if cfg.URL != "" {
		return cfg.URL
	}
	return steamAPIBase
}

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
		return fmt.Errorf("GET %s: %s", urlWithoutQuery(rawURL), resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode %s: %w", rawURL, err)
	}
	return nil
}

// urlWithoutQuery drops the query string, which carries the API token.
func urlWithoutQuery(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	u.RawQuery = ""
	return u.String()
}

func getPlayerSummary(ctx context.Context, hc *http.Client, cfg Config) (playerSummariesResp, error) {
	u := fmt.Sprintf("%s/ISteamUser/GetPlayerSummaries/v2/?key=%s&steamids=%s",
		apiBase(cfg), url.QueryEscape(cfg.Token), url.QueryEscape(cfg.User))
	var out playerSummariesResp
	if err := doJSON(ctx, hc, u, &out); err != nil {
		return out, err
	}
	return out, nil
}

func getLevel(ctx context.Context, hc *http.Client, cfg Config) (steamLevelResp, error) {
	u := fmt.Sprintf("%s/IPlayerService/GetSteamLevel/v1/?key=%s&steamid=%s",
		apiBase(cfg), url.QueryEscape(cfg.Token), url.QueryEscape(cfg.User))
	var out steamLevelResp
	if err := doJSON(ctx, hc, u, &out); err != nil {
		return out, err
	}
	return out, nil
}

func getOwnedGames(ctx context.Context, hc *http.Client, cfg Config) (ownedGamesResp, error) {
	u := fmt.Sprintf("%s/IPlayerService/GetOwnedGames/v1/?key=%s&steamid=%s&include_appinfo=1",
		apiBase(cfg), url.QueryEscape(cfg.Token), url.QueryEscape(cfg.User))
	var out ownedGamesResp
	if err := doJSON(ctx, hc, u, &out); err != nil {
		return out, err
	}
	return out, nil
}

func getRecent(ctx context.Context, hc *http.Client, cfg Config) (recentGamesResp, error) {
	u := fmt.Sprintf("%s/IPlayerService/GetRecentlyPlayedGames/v1/?key=%s&steamid=%s",
		apiBase(cfg), url.QueryEscape(cfg.Token), url.QueryEscape(cfg.User))
	var out recentGamesResp
	if err := doJSON(ctx, hc, u, &out); err != nil {
		return out, err
	}
	return out, nil
}

// Success is false when the game has no achievement schema or the profile is
// private; Achievements is then empty.
type playerAchievementsResp struct {
	PlayerStats struct {
		Success      bool `json:"success"`
		Achievements []struct {
			Achieved int `json:"achieved"`
		} `json:"achievements"`
	} `json:"playerstats"`
}

// Best-effort: Steam answers 4xx for games without achievements or private
// profiles, returned here as an error the caller is expected to swallow.
func getAchievements(ctx context.Context, hc *http.Client, cfg Config, appid int) (unlocked, total int, err error) {
	u := fmt.Sprintf("%s/ISteamUserStats/GetPlayerAchievements/v0001/?key=%s&steamid=%s&appid=%d&l=en",
		apiBase(cfg), url.QueryEscape(cfg.Token), url.QueryEscape(cfg.User), appid)
	var out playerAchievementsResp
	if err := doJSON(ctx, hc, u, &out); err != nil {
		return 0, 0, err
	}
	if !out.PlayerStats.Success {
		return 0, 0, nil
	}
	for _, a := range out.PlayerStats.Achievements {
		if a.Achieved == 1 {
			unlocked++
		}
	}
	return unlocked, len(out.PlayerStats.Achievements), nil
}

// hash is the bare img_icon_url value; the JPG lives under media.steampowered.com.
func iconURL(appid int, hash string) string {
	if hash == "" {
		return ""
	}
	return fmt.Sprintf("https://media.steampowered.com/steamcommunity/public/images/apps/%d/%s.jpg", appid, hash)
}
