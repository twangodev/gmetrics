package render

// ClassicCSS is the embedded <style> block for the outer SVG. The values are
// written inline (not loaded from an external stylesheet) so they survive
// GitHub's Camo image proxy, which strips external references and script
// content from SVGs served through `<img>` tags.
const ClassicCSS = `
:root {
  --color-text: #24292f;
  --color-muted: #57606a;
  --color-bg: #ffffff;
  --color-border: #d0d7de;
  --color-accent: #0969da;
  --color-error: #cf222e;
  --color-error-bg: #ffebe9;
}
@media (prefers-color-scheme: dark) {
  :root {
    --color-text: #c9d1d9;
    --color-muted: #8b949e;
    --color-bg: #0d1117;
    --color-border: #30363d;
    --color-accent: #58a6ff;
    --color-error: #ff7b72;
    --color-error-bg: #490202;
  }
}
.frame { fill: var(--color-bg); stroke: var(--color-border); }
.text-muted { fill: var(--color-muted); }
.text-error { fill: var(--color-error); }
.bg-error    { fill: var(--color-error-bg); }
`
