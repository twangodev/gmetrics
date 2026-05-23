# gmetrics

`gmetrics` is a Go port of [lowlighter/metrics](https://github.com/lowlighter/metrics) focused on the SVG output path. It is built on a small set of well-maintained libraries to escape the maintenance fragility of the upstream Node-plus-Puppeteer stack. The whole thing ships as a single static Go binary; the GitHub Action wraps that same binary in a roughly 25 MB Docker image with no headless browser involved.

This is **not** a feature-for-feature reimplementation. It ports the slice of upstream the maintainer actually uses in production, with the explicit goal of being boring and durable.

---

## In-scope feature matrix

| Plugin      | What it shows                                                                                              |
| ----------- | ---------------------------------------------------------------------------------------------------------- |
| `base`      | header, activity, community, repositories, metadata sections                                               |
| `languages` | most-used languages bar with optional `indepth` git + linguist analysis                                    |
| `people`    | followers and following avatar grids                                                                       |
| `wakatime`  | time totals plus per-category bar charts (projects, languages, editors, os, with `-graphs` variants)       |
| `music`     | Last.fm recent tracks                                                                                      |
| `steam`     | player profile plus most-played and recently-played games                                                  |

**v1 supports the `classic` template only, `svg` output only, and `output_action: none` only.** Other upstream plugins, templates, output formats, and output modes are intentionally out of scope. The `music` plugin supports the `lastfm` provider only (no Spotify, YouTube, Apple Music).

For the full out-of-scope list, see [`docs/superpowers/specs/2026-05-22-gmetrics-port-design.md`](docs/superpowers/specs/2026-05-22-gmetrics-port-design.md) section 13.

---

## Quickstart — local CLI

Build and run:

```bash
# Build the binary
go build -o gmetrics ./cmd/gmetrics

# Render from a YAML config
GITHUB_TOKEN=$(gh auth token) \
  ./gmetrics render -c card.yaml -o github-metrics.svg
```

A small `card.yaml` (mirrors [`testdata/main.yaml`](testdata/main.yaml)):

```yaml
user: twangodev
filename: github-metrics.svg

base:
  sections: [header, activity, community, repositories, metadata]
  hireable: true
  indepth: true
  commits_authoring:
    - .user.login
    - 48845764+twangodev@users.noreply.github.com
  repositories:
    affiliations: [owner, collaborator, organization_member]
    max: 1000
    batch: 50

plugins:
  languages:
    enabled: true
    sections: [most-used]
    details: [percentage]
    ignored: [markdown]
    other: false
    limit: 8

  people:
    enabled: true
    types: [followers, following]
    limit: 24
    size: 28
```

The CLI supports a few flags on top of the config file:

| Flag             | Purpose                                                       |
| ---------------- | ------------------------------------------------------------- |
| `-c, --config`   | Path to YAML config file (optional; env vars overlay)         |
| `-o, --output`   | Output SVG path (overrides `filename` in config)              |
| `--strict`       | Fail the whole render on any plugin error (default: degrade)  |
| `-v, --verbose`  | Verbose (debug-level) logging                                 |

Configuration sources are merged in priority order: defaults < YAML file < `INPUT_*` env vars < CLI flags.

---

## Quickstart — GitHub Action

```yaml
- uses: twangodev/gmetrics@v1   # placeholder; pin to a real ref
  with:
    token: ${{ secrets.METRICS_TOKEN || secrets.GITHUB_TOKEN }}
    user: ${{ github.repository_owner }}
    filename: github-metrics.svg
    output_action: none
    base: 'header, activity, community, repositories, metadata'
    base_hireable: 'yes'
    base_indepth: 'yes'
    plugin_languages: 'yes'
    plugin_languages_ignored: 'markdown'
    plugin_languages_sections: 'most-used'
    plugin_languages_details: 'percentage'
    plugin_people: 'yes'
    plugin_people_types: 'followers, following'
    plugin_people_limit: '24'
    plugin_people_size: '28'
```

`output_action: none` writes the SVG into the runner's workspace. To publish it back to the repo, follow up with an explicit commit step, for example:

```yaml
- run: |
    git config user.name "github-actions[bot]"
    git config user.email "github-actions[bot]@users.noreply.github.com"
    git add github-metrics.svg
    git commit -m "chore: refresh metrics" || echo "no changes"
    git push
```

Or use a dedicated commit action such as `stefanzweifel/git-auto-commit-action`. Other upstream output modes (`commit`, `pull-request`, `gist`) are out of scope for v1.

The action's full input list is declared in [`action/action.yml`](action/action.yml).

---

## Configuration reference

The full config schema lives in [`internal/config/config.go`](internal/config/config.go). The mapping from `INPUT_*` env vars to nested YAML keys is in [`internal/config/env.go`](internal/config/env.go). The complete table of inputs accepted by the GitHub Action is in [`action/action.yml`](action/action.yml).

Boolean values accept any of `yes` / `no` / `true` / `false` / `on` / `off` / `1` / `0`. Comma-separated strings are split into slices for fields typed as `[]string`.

---

## Architecture

Six plugins live as separate packages under [`internal/plugins/`](internal/plugins/) (`base`, `languages`, `people`, `wakatime`, `music`, `steam`). Each plugin registers itself with the central registry in its package `init()` via a side-effect import from [`cmd/gmetrics/render.go`](cmd/gmetrics/render.go), so adding or removing a plugin is one import line.

The engine in [`internal/metrics/`](internal/metrics/) runs `base` first because it populates the shared `Env.User` context (login, name, totals) that every other plugin reads. It then fans the remaining enabled plugins out via `errgroup` for parallel `Fetch`, and finally calls each plugin's `Render` in a fixed display order so the final card layout is deterministic.

Rendering is pure SVG via [`tdewolff/canvas`](https://github.com/tdewolff/canvas). The bundled Inter font is parsed once at startup and every text run is converted to outline `<path>` data at export time. This matters because GitHub serves SVGs through the Camo image proxy, which strips `<foreignObject>`, scripts, and `@font-face` data-URI references — so a card that "looks fine in a browser tab" can silently break in a README. Outline-mode SVG survives Camo unchanged.

Configuration is layered defaults < YAML file < `INPUT_*` env vars < CLI flags, merged by `koanf/v2`.

---

## Why not upstream

Upstream `lowlighter/metrics` is comprehensive, supports 40-plus plugins, and renders pixel-perfect HTML-driven layouts. It does this by carrying Node, Puppeteer, headless Chromium, Sharp, and a long tail of transitive dependencies in varying states of maintenance, and the resulting Docker image is around 500 MB.

`gmetrics` is the opposite trade. It ships only the slice this project's maintainer actually uses, refuses to embed a browser, and stays small enough to be boring infrastructure: a roughly 25 MB image, a single static Go binary, sub-100 ms render times, and a dependency list short enough to audit in an afternoon. If you need the full upstream feature set, use upstream. If you want the SVG card path on a maintenance budget you can defend, use this.

---

## Development

```bash
go test ./... -race                   # full test suite (unit + golden snapshots)
go build ./...                        # check everything compiles
go run ./cmd/gmetrics render --help   # CLI help
```

Golden SVG snapshots are maintained via the standard `goldie/v2` `-update` flag:

```bash
go test ./... -update                 # regenerate goldens after intentional layout changes
```

The Inter font files used at render time are bundled at [`internal/render/fonts/Inter-Regular.otf`](internal/render/fonts/Inter-Regular.otf) and [`internal/render/fonts/Inter-Bold.otf`](internal/render/fonts/Inter-Bold.otf) under the SIL Open Font License 1.1.

Key reference docs:

- Design spec: [`docs/superpowers/specs/2026-05-22-gmetrics-port-design.md`](docs/superpowers/specs/2026-05-22-gmetrics-port-design.md)
- Implementation plan: [`docs/superpowers/plans/2026-05-22-gmetrics-port.md`](docs/superpowers/plans/2026-05-22-gmetrics-port.md)
- Example configs: [`testdata/main.yaml`](testdata/main.yaml), [`testdata/sidebar.yaml`](testdata/sidebar.yaml)
- Upstream (read-only reference): `reference/lowlighter-metrics/`

---

## License

MIT for this project (see [`LICENSE`](LICENSE)). The bundled Inter font in [`internal/render/fonts/`](internal/render/fonts/) is distributed under the SIL Open Font License 1.1; see [`internal/render/fonts/`](internal/render/fonts/) for that license.
