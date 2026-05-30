package languages

import (
	"strings"

	enry "github.com/go-enry/go-enry/v2"
)

const neutralColor = "#cccccc"

// Values are taken verbatim from GitHub Linguist's languages.yml.
var defaultColors = map[string]string{
	"Go":               "#00ADD8",
	"Python":           "#3572A5",
	"JavaScript":       "#f1e05a",
	"TypeScript":       "#3178c6",
	"Java":             "#b07219",
	"C":                "#555555",
	"C++":              "#f34b7d",
	"C#":               "#178600",
	"Ruby":             "#701516",
	"Rust":             "#dea584",
	"Swift":            "#F05138",
	"Kotlin":           "#A97BFF",
	"PHP":              "#4F5D95",
	"HTML":             "#e34c26",
	"CSS":              "#663399",
	"SCSS":             "#c6538c",
	"Shell":            "#89e051",
	"Lua":              "#000080",
	"Vue":              "#41b883",
	"Svelte":           "#ff3e00",
	"Dart":             "#00B4AB",
	"Elixir":           "#6e4a7e",
	"Erlang":           "#B83998",
	"Haskell":          "#5e5086",
	"Markdown":         "#083fa1",
	"Dockerfile":       "#384d54",
	"Makefile":         "#427819",
	"YAML":             "#cb171e",
	"JSON":             "#292929",
	"R":                "#198CE7",
	"Scala":            "#c22d40",
	"Perl":             "#0298c3",
	"OCaml":            "#3be133",
	"Zig":              "#ec915c",
	"Nix":              "#7e7eff",
	"Vim Script":       "#199f4b",
	"PowerShell":       "#012456",
	"Objective-C":      "#438eff",
	"Jupyter Notebook": "#DA5B0B",
}

func ColorFor(name string) string {
	return colorFor(name, nil)
}

func colorFor(name string, overrides map[string]string) string {
	if overrides != nil {
		if c, ok := overrides[name]; ok && c != "" {
			return c
		}
	}
	if c, ok := defaultColors[name]; ok && c != "" {
		return c
	}
	if c := enry.GetColor(name); c != "" {
		return c
	}
	return neutralColor
}

func isIgnored(name string, ignored []string) bool {
	if len(ignored) == 0 {
		return false
	}
	lowerName := strings.ToLower(name)
	for _, ig := range ignored {
		if strings.ToLower(ig) == lowerName {
			return true
		}
	}
	return false
}
