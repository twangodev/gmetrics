# gmetrics

![Go version](https://img.shields.io/github/go-mod/go-version/twangodev/gmetrics)
![Downloads](https://img.shields.io/github/downloads/twangodev/gmetrics/total)
![Build](https://img.shields.io/github/actions/workflow/status/twangodev/gmetrics/go.yaml?branch=main)
![License](https://img.shields.io/github/license/twangodev/gmetrics)

A Go port of [lowlighter/metrics](https://github.com/lowlighter/metrics) for the SVG output path.

## Caching indepth language stats

`plugin_languages_indepth` keeps an incremental cache so each run only processes
commits added since the previous run. Persist it across runs with `actions/cache`:

```yaml
- uses: actions/cache@v4
  with:
    path: .gmetrics-cache
    key: gmetrics-languages-${{ github.run_id }}
    restore-keys: gmetrics-languages-
- uses: twangodev/gmetrics@v1
  with:
    token: ${{ secrets.METRICS_TOKEN }}
    plugin_languages: 'yes'
    plugin_languages_indepth: 'yes'
```
