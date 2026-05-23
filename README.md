# gmetrics

A small Go port of [lowlighter/metrics](https://github.com/lowlighter/metrics) focused on the SVG output path. Single static binary, no headless browser, ~25 MB Docker image.

## Scope

| Plugin      | What it shows                                                          |
| ----------- | ---------------------------------------------------------------------- |
| `base`      | header, activity, community, repositories, metadata                    |
| `languages` | most-used languages bar (optional `indepth` git + linguist analysis)   |
| `people`    | followers and following avatar grids                                   |
| `wakatime`  | time totals and per-category bar charts                                |
| `music`     | Last.fm recent tracks                                                  |
| `steam`     | player profile, most-played, recently-played                           |

v1 supports the `classic` template, `svg` output, `output_action: none`, and the `lastfm` music provider only.

## CLI

```bash
go build -o gmetrics ./cmd/gmetrics

GITHUB_TOKEN=$(gh auth token) \
  ./gmetrics render -c card.yaml -o github-metrics.svg
```

See [`testdata/main.yaml`](testdata/main.yaml) for a sample config.

Flags: `-c/--config`, `-o/--output`, `--strict`, `-v/--verbose`. Config sources merge in priority order: defaults < YAML file < `INPUT_*` env vars < CLI flags.

## GitHub Action

```yaml
- uses: twangodev/gmetrics@v1
  with:
    token: ${{ secrets.GITHUB_TOKEN }}
    user: ${{ github.repository_owner }}
    filename: github-metrics.svg
    plugin_languages: 'yes'
    plugin_people: 'yes'
```

Full input list in [`action/action.yml`](action/action.yml). `output_action: none` writes to the workspace; commit the SVG back with a separate step (e.g. `stefanzweifel/git-auto-commit-action`).

## Architecture

Plugins live under [`internal/plugins/`](internal/plugins/) and register themselves in `init()`. The engine ([`internal/metrics/`](internal/metrics/)) runs `base` first to populate the shared user context, then fans the rest out with `errgroup`. Rendering is pure SVG via [`tdewolff/canvas`](https://github.com/tdewolff/canvas); the bundled Inter font is converted to outline `<path>` data so output survives GitHub's Camo proxy unchanged.

## Development

```bash
go test ./... -race
go test ./... -update    # refresh golden SVG snapshots
```

## License

MIT (this project). Inter font under SIL OFL 1.1, see [`internal/render/fonts/`](internal/render/fonts/).
