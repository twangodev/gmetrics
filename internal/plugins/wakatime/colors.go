package wakatime

import (
	"hash/fnv"
	"image/color"
	"math"
	"strings"

	languagespkg "github.com/twangodev/gmetrics/internal/plugins/languages"
)

// editorColors maps WakaTime editor identifiers to canonical brand colors.
// Keys are matched case-insensitively. Provenance per category:
//
//   - JetBrains IDEs (IntelliJ, PyCharm, WebStorm, GoLand, etc.) — pulled
//     from the official github.com/JetBrains/logos SVGs (dominant gradient
//     stop, since the marketing logo is a multi-stop cube and Simple Icons
//     collapses them all to #000 wordmark).
//   - VS Code, Visual Studio — code.visualstudio.com/brand and Microsoft
//     brand guidelines; not in Simple Icons (MS trademark policy).
//   - Most others — Simple Icons (simpleicons.org), the de-facto registry
//     of brand-registered hex codes.
//   - Atom, Brackets — Wikimedia Commons SVGs (no longer in Simple Icons).
//   - Claude Code — Anthropic's Claude brand "Crail" orange.
//   - BBEdit / Nova / TextMate — no published brand kit; sampled from the
//     macOS app icon. Replace if a vendor source surfaces.
var editorColors = map[string]string{
	// Microsoft
	"vs code":       "#0098ff",
	"vscode":        "#0098ff",
	"visual studio": "#5c2d91",

	// JetBrains family (dominant gradient stops)
	"intellij idea": "#fe2857",
	"intellij":      "#fe2857",
	"pycharm":       "#00d886",
	"webstorm":      "#00c4f4",
	"goland":        "#007dfe",
	"phpstorm":      "#ff2d90",
	"rubymine":      "#ff2358",
	"clion":         "#00d980",
	"rider":         "#ff0a67",
	"datagrip":      "#7256ff",
	"appcode":       "#087cfa",
	"rustrover":     "#ffab00",
	"dataspell":     "#00d980",
	"aqua":          "#007dfe",
	"fleet":         "#0500ff",
	"mps":           "#21d789",

	// AI-first editors
	"claude code": "#d97757",
	"cursor":      "#000000",
	"zed":         "#084ccf",
	"windsurf":    "#0b100f",

	// Classic editors
	"sublime text": "#ff9800",
	"atom":         "#66595c",
	"vim":          "#019733",
	"neovim":       "#57a143",
	"emacs":        "#7f5ab6",
	"textmate":     "#172733",
	"bbedit":       "#9591d7",
	"brackets":     "#1d9cd7",
	"notepad++":    "#90e59a",
	"eclipse":      "#2c2255",
	"nova":         "#1e3148",

	// Mobile / native
	"android studio": "#3ddc84",
	"xcode":          "#147efb",

	// Sentinel for WakaTime's "name unset" bin
	"unknown editor": "#9aa0a6",
}

// osColors maps WakaTime OS identifiers to common brand colors.
var osColors = map[string]string{
	"linux":     "#fcc624",
	"mac":       "#a2aaad",
	"macos":     "#a2aaad",
	"darwin":    "#a2aaad",
	"windows":   "#0078d6",
	"android":   "#3ddc84",
	"ios":       "#000000",
	"ipados":    "#000000",
	"chrome os": "#1a73e8",
	"freebsd":   "#ab2b28",
	"unknown":   "#9aa0a6",
}

// barColorFor returns the bar color for one row in the named category.
// Language rows route through the languages plugin's Linguist resolver;
// editor / OS rows look up curated brand colors; project rows hash to a
// stable HSL hue so each project gets a distinct-but-deterministic color.
// Anything unresolved falls back to a palette cycle on i.
func barColorFor(category, name string, i int) color.RGBA {
	switch category {
	case "languages":
		if hex := languagespkg.ColorFor(name); hex != "" {
			if c, ok := parseHex(hex); ok {
				return c
			}
		}
	case "editors":
		if hex, ok := editorColors[strings.ToLower(name)]; ok {
			if c, ok := parseHex(hex); ok {
				return c
			}
		}
	case "os":
		if hex, ok := osColors[strings.ToLower(name)]; ok {
			if c, ok := parseHex(hex); ok {
				return c
			}
		}
	case "projects":
		return projectColor(name)
	}
	return barPalette[i%len(barPalette)]
}

// projectColor hashes a project name to a stable HSL hue and returns the
// RGBA. Saturation and lightness are fixed so colors stay visually balanced
// against the muted card background regardless of the hue.
func projectColor(name string) color.RGBA {
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(name)))
	hue := float64(h.Sum32()%360) / 360.0
	return hslToRGBA(hue, 0.55, 0.55)
}

// parseHex accepts "#rrggbb" or "rrggbb" and returns the opaque RGBA.
func parseHex(s string) (color.RGBA, bool) {
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return color.RGBA{}, false
	}
	var rgb [3]uint8
	for i := 0; i < 3; i++ {
		hi, ok1 := hexNibble(s[i*2])
		lo, ok2 := hexNibble(s[i*2+1])
		if !ok1 || !ok2 {
			return color.RGBA{}, false
		}
		rgb[i] = hi<<4 | lo
	}
	return color.RGBA{R: rgb[0], G: rgb[1], B: rgb[2], A: 0xff}, true
}

func hexNibble(c byte) (uint8, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// hslToRGBA converts HSL in [0,1] to opaque RGBA, using the standard
// formula (https://www.w3.org/TR/css-color-3/#hsl-color).
func hslToRGBA(h, s, l float64) color.RGBA {
	if s == 0 {
		v := uint8(math.Round(l * 255))
		return color.RGBA{R: v, G: v, B: v, A: 0xff}
	}
	var q float64
	if l < 0.5 {
		q = l * (1 + s)
	} else {
		q = l + s - l*s
	}
	p := 2*l - q
	r := hueToRGB(p, q, h+1.0/3.0)
	g := hueToRGB(p, q, h)
	b := hueToRGB(p, q, h-1.0/3.0)
	return color.RGBA{
		R: uint8(math.Round(r * 255)),
		G: uint8(math.Round(g * 255)),
		B: uint8(math.Round(b * 255)),
		A: 0xff,
	}
}

func hueToRGB(p, q, t float64) float64 {
	if t < 0 {
		t += 1
	}
	if t > 1 {
		t -= 1
	}
	switch {
	case t < 1.0/6.0:
		return p + (q-p)*6*t
	case t < 1.0/2.0:
		return q
	case t < 2.0/3.0:
		return p + (q-p)*(2.0/3.0-t)*6
	}
	return p
}
