package render

// ClassicCSS is the embedded <style> block for the outer SVG. The values are
// written inline (not loaded from an external stylesheet) so they survive
// GitHub's Camo image proxy, which strips external references and script
// content from SVGs served through `<img>` tags.
//
// Colors mirror upstream lowlighter/metrics classic template
// (reference/lowlighter-metrics/source/templates/classic/style.css):
// default text #777777, headings/accent #0366d6, muted #666666,
// icon fill #959da5. The card is light-mode only — upstream has no
// prefers-color-scheme: dark rules, so neither do we; this keeps the
// rendered SVG visually identical whether the viewer is in light or
// dark mode.
const ClassicCSS = `
text {
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif, "Apple Color Emoji", "Segoe UI Emoji";
  fill: #777777;
}
.frame { fill: none; stroke: #d0d7de; }
.text-h1      { fill: #0366d6; }
.text-heading { fill: #0366d6; }
.text-muted   { fill: #666666; }
.text-icon    { fill: #959da5; }
.text-accent  { fill: #0366d6; }
.text-error   { fill: #cf222e; }
.bg-error     { fill: #ffebe9; }
`
