package wakatime

import (
	"hash/fnv"
	"image/color"
	"math"
	"strings"

	languagespkg "github.com/twangodev/gmetrics/internal/plugins/languages"
)

// editorColors keys are lowercased; callers must ToLower before lookup.
// Provenance: JetBrains uses the dominant gradient stop; Microsoft editors come from brand
// kits (absent from Simple Icons); BBEdit/Nova/TextMate are sampled from the app icon — replace if a vendor source surfaces.
var editorColors = map[string]string{
	"vs code":       "#0098ff",
	"vscode":        "#0098ff",
	"visual studio": "#5c2d91",

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

	"claude code": "#d97757",
	"cursor":      "#000000",
	"zed":         "#084ccf",
	"windsurf":    "#0b100f",

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

	"android studio": "#3ddc84",
	"xcode":          "#147efb",

	"unknown editor": "#9aa0a6",
}

// osColors keys are lowercased; callers must ToLower before lookup.
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

const (
	projectSaturation = 0.55
	projectLightness  = 0.55
)

func projectColor(name string) color.RGBA {
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(name)))
	hue := float64(h.Sum32()%360) / 360.0
	return hslToRGBA(hue, projectSaturation, projectLightness)
}

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

// hslToRGBA: h, s, l in [0,1] per https://www.w3.org/TR/css-color-3/#hsl-color.
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
