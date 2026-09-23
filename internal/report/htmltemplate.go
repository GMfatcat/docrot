package report

import "html/template"

// htmlTemplate renders htmlView. Everything the page needs — inline CSS and
// the filter script — travels with it, so the file works offline and can be
// mailed around as a single attachment.
var htmlTemplate = template.Must(template.New("report").Parse(htmlSource))

// htmlSource is the single-file report page.
const htmlSource = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>docrot report</title>
<style>
:root {
  --bg: #ffffff; --fg: #16181d; --muted: #656a75; --line: #e2e5ea;
  --card: #f6f7f9; --code: #f0f2f5;
  --error: #c0392b; --warning: #9a6b00; --info: #1f6f8b; --ok: #2e7d4f;
}
@media (prefers-color-scheme: dark) {
  :root {
    --bg: #14161a; --fg: #e6e8ec; --muted: #9aa1ad; --line: #2b2f36;
    --card: #1c1f25; --code: #22262d;
    --error: #ef6b5e; --warning: #e0a33a; --info: #69b7d0; --ok: #62c48c;
  }
}
* { box-sizing: border-box; }
body {
  margin: 0; padding: 0 16px 48px; background: var(--bg); color: var(--fg);
  font: 15px/1.5 system-ui, -apple-system, Segoe UI, Roboto, sans-serif;
}
.wrap { max-width: 1100px; margin: 0 auto; }
header { padding: 24px 0 8px; border-bottom: 1px solid var(--line); }
h1 { font-size: 22px; margin: 0 0 4px; }
h2 { font-size: 17px; margin: 32px 0 8px; }
.sub { color: var(--muted); font-size: 13px; word-break: break-all; }
.cards { display: flex; flex-wrap: wrap; gap: 8px; margin: 16px 0; }
.card {
  flex: 1 1 110px; min-width: 96px; padding: 10px 12px;
  background: var(--card); border: 1px solid var(--line); border-radius: 8px;
}
.card .v { font-size: 20px; font-weight: 600; }
.card .l { font-size: 12px; color: var(--muted); text-transform: uppercase; letter-spacing: 0.04em; }
.card.error .v { color: var(--error); }
.card.warning .v { color: var(--warning); }
.card.info .v { color: var(--info); }
.card.muted .v { color: var(--muted); }
.filters {
  display: flex; flex-wrap: wrap; gap: 10px; align-items: center;
  padding: 10px 0; border-bottom: 1px solid var(--line);
  position: sticky; top: 0; background: var(--bg); z-index: 2;
}
.filters label { font-size: 13px; display: inline-flex; align-items: center; gap: 4px; }
.filters input, .filters select {
  padding: 5px 8px; border: 1px solid var(--line); border-radius: 6px;
  background: var(--bg); color: var(--fg); font: inherit; font-size: 13px;
}
.filters input.q { flex: 1 1 200px; min-width: 140px; }
.count { color: var(--muted); font-size: 13px; }
.file-group { margin-top: 20px; }
.file-group h3 {
  font: 600 14px/1.4 ui-monospace, SFMono-Regular, Consolas, monospace;
  margin: 0 0 6px; word-break: break-all;
}
table { width: 100%; border-collapse: collapse; }
td { padding: 6px 8px; border-top: 1px solid var(--line); vertical-align: top; font-size: 14px; }
td.pos { font-family: ui-monospace, SFMono-Regular, Consolas, monospace; color: var(--muted); white-space: nowrap; }
td.rule { font-size: 12px; color: var(--muted); white-space: nowrap; }
.badge {
  display: inline-block; padding: 1px 7px; border-radius: 999px;
  font-size: 12px; font-weight: 600; border: 1px solid currentColor; white-space: nowrap;
}
.badge.error { color: var(--error); }
.badge.warning { color: var(--warning); }
.badge.info { color: var(--info); }
.suggestion { color: var(--ok); }
.tag { font-size: 11px; color: var(--muted); border: 1px solid var(--line); border-radius: 4px; padding: 0 4px; }
.ctx {
  display: block; margin-top: 4px; padding: 4px 6px; background: var(--code);
  border-radius: 4px; font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-size: 12px; white-space: pre-wrap; word-break: break-word; color: var(--muted);
}
.bar { height: 8px; background: var(--code); border-radius: 999px; overflow: hidden; margin-top: 4px; }
.bar i { display: block; height: 100%; background: var(--ok); }
.cov { margin: 10px 0; }
.cov .head { display: flex; justify-content: space-between; gap: 8px; font-size: 14px; }
.cov .name { font-family: ui-monospace, SFMono-Regular, Consolas, monospace; word-break: break-all; }
.cov .miss { color: var(--muted); font-size: 12px; margin-top: 4px; word-break: break-word; }
.empty { color: var(--muted); padding: 16px 0; }
footer { margin-top: 40px; padding-top: 12px; border-top: 1px solid var(--line); color: var(--muted); font-size: 12px; }
@media (max-width: 560px) {
  td { display: block; border-top: none; padding: 2px 0; }
  tr.finding { display: block; border-top: 1px solid var(--line); padding: 8px 0; }
}
</style>
</head>
<body>
<div class="wrap">

<header>
  <h1>docrot report</h1>
  <div class="sub">{{with .Version}}version {{.}} &middot; {{end}}root {{if .Root}}{{.Root}}{{else}}(unknown){{end}}</div>
</header>

<div class="cards">
{{range .Cards}}  <div class="card {{.Tone}}"><div class="v">{{.Value}}</div><div class="l">{{.Label}}</div></div>
{{end}}</div>

<h2>Findings</h2>
<div class="filters">
  <label><input type="checkbox" class="sev-filter" value="error" checked> error</label>
  <label><input type="checkbox" class="sev-filter" value="warning" checked> warning</label>
  <label><input type="checkbox" class="sev-filter" value="info" checked> info</label>
  <label>rule
    <select id="rule">
      <option value="">all</option>
{{range .Rules}}      <option value="{{.}}">{{.}}</option>
{{end}}    </select>
  </label>
  <input type="text" class="q" id="q" placeholder="filter file or message">
  <span class="count"><span id="count">{{.Shown}}</span> shown{{if .Hidden}} &middot; {{.Hidden}} baselined hidden{{end}}</span>
</div>

{{if .Files}}
{{range .Files}}<section class="file-group">
  <h3>{{.File}}</h3>
  <table>
{{range .Items}}    <tr class="finding" data-sev="{{.Severity}}" data-rule="{{.Rule}}" data-text="{{.Filter}}">
      <td class="pos">{{.Pos}}</td>
      <td><span class="badge {{.Severity}}">{{.Severity}}</span></td>
      <td class="rule">{{.Rule}}</td>
      <td>
        {{if .Baselined}}<span class="tag">baselined</span> {{end}}{{.Message}}
        {{if .Suggestion}}<span class="suggestion">(did you mean {{.Suggestion}}?)</span>{{end}}
        {{if .Context}}<code class="ctx">{{.Context}}</code>{{end}}
      </td>
    </tr>
{{end}}  </table>
</section>
{{end}}
{{else}}<p class="empty">No findings.</p>
{{end}}

{{if .Pairs}}
<h2>Pairs</h2>
<table>
{{range .Pairs}}  <tr class="pair">
    <td class="pos">{{.File}}</td>
    <td><span class="badge {{.Severity}}">{{.Severity}}</span></td>
    <td class="rule">{{.Rule}}</td>
    <td>{{.Message}}</td>
  </tr>
{{end}}</table>
{{end}}

{{with .Coverage}}
<h2>Coverage</h2>
{{range .Packages}}{{template "covrow" .}}{{end}}
{{template "covrow" .Flags}}
{{template "covrow" .Envs}}
{{with .Routes}}{{template "covrow" .}}{{end}}
{{with .Configs}}{{template "covrow" .}}{{end}}
{{end}}

<footer>Generated by docrot{{with .Version}} {{.}}{{end}} &mdash; documentation rot detector. No external resources.</footer>
</div>

<script>
(function () {
  var q = document.getElementById('q');
  var rule = document.getElementById('rule');
  var counter = document.getElementById('count');
  var sevs = Array.prototype.slice.call(document.querySelectorAll('.sev-filter'));
  var rows = Array.prototype.slice.call(document.querySelectorAll('tr.finding'));
  var groups = Array.prototype.slice.call(document.querySelectorAll('.file-group'));

  function apply() {
    var text = q.value.toLowerCase().trim();
    var wanted = {};
    sevs.forEach(function (c) { wanted[c.value] = c.checked; });
    var want = rule.value;
    rows.forEach(function (row) {
      var ok = wanted[row.getAttribute('data-sev')] !== false;
      if (ok && want && row.getAttribute('data-rule') !== want) { ok = false; }
      if (ok && text && row.getAttribute('data-text').indexOf(text) === -1) { ok = false; }
      row.hidden = !ok;
    });
    var shown = 0;
    groups.forEach(function (g) {
      var visible = g.querySelectorAll('tr.finding:not([hidden])').length;
      g.hidden = visible === 0;
      shown += visible;
    });
    if (counter) { counter.textContent = String(shown); }
  }

  q.addEventListener('input', apply);
  rule.addEventListener('change', apply);
  sevs.forEach(function (c) { c.addEventListener('change', apply); });
  apply();
})();
</script>
</body>
</html>
{{define "covrow"}}<div class="cov">
  <div class="head"><span class="name">{{.Name}}</span><span>{{.Documented}}/{{.Total}} ({{.Pct}}%)</span></div>
  <div class="bar"><i style="{{.BarStyle}}"></i></div>
  {{if .Missing}}<div class="miss">missing: {{range $i, $m := .Missing}}{{if $i}}, {{end}}{{$m}}{{end}}{{if .More}}, and {{.More}} more{{end}}</div>{{end}}
</div>
{{end}}
`
