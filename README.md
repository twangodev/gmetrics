# gmetrics

![Go version](https://img.shields.io/github/go-mod/go-version/twangodev/gmetrics)
![Downloads](https://img.shields.io/github/downloads/twangodev/gmetrics/total)
![Build](https://img.shields.io/github/actions/workflow/status/twangodev/gmetrics/go.yaml?branch=main)
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

When `plugin_languages_indepth` is enabled the action caches per-repo language
stats across runs automatically, so each run only processes commits added since
the previous one. No extra workflow steps are required.
