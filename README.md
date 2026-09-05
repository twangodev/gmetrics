# gmetrics

![Go version](https://img.shields.io/github/go-mod/go-version/twangodev/gmetrics)
![Release](https://img.shields.io/github/v/release/twangodev/gmetrics)
![Build](https://img.shields.io/github/actions/workflow/status/twangodev/gmetrics/go.yml?branch=main)
![License](https://img.shields.io/github/license/twangodev/gmetrics)

A Go port of [lowlighter/metrics](https://github.com/lowlighter/metrics) for the SVG output path.

## Usage

```yaml
- uses: twangodev/gmetrics@v1
  with:
    token: ${{ secrets.METRICS_TOKEN }}
    plugin_languages: 'yes'
    plugin_languages_indepth: 'yes'
```

The `v1` tag follows the latest compatible v1 release. The action runs the
container tagged with the requested ref: `v1`, a full release tag such as
`v1.8.0`, `main`, or a full commit SHA successfully published from `main`.
Other branches, abbreviated SHAs, and local-path usage are unsupported.
Matching images must exist before use; older releases and commits are not
backfilled by this workflow. Pin a full published commit SHA to fix the action
source version; container tags are not digest pins. Running the action requires
a Linux runner with Docker.

When `plugin_languages_indepth` is enabled the action caches per-repo language
stats across runs automatically, so each run only processes commits added since
the previous one. No extra workflow steps are required.

In-depth language totals use the repository-root `exclusion.toml` file to omit
imported or generated assets. Set `GMETRICS_EXCLUSION_PATH` to use a different
file; the default template excludes common bundles and dependency trees.
