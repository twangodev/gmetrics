package render

// Styles are inline (not external) to survive GitHub's Camo proxy, which strips
// external references from SVGs served via <img>. Light-mode only, matching upstream.
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
