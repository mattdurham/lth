// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package metrics

import (
	"fmt"
	"net/http"
)

// handleUI serves the search web UI as a self-contained HTML page.
// The UI fires parallel per-layer search requests — one "agent" per layer —
// and populates each panel independently as each resolves.
func handleUI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprint(w, uiHTML) //nolint:errcheck
}

const uiHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>lth — memory search</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:monospace;background:#0d0d0d;color:#e0e0e0;min-height:100vh;padding:24px}
h1{font-size:1.2rem;color:#7eb8f7;margin-bottom:4px}
.subtitle{color:#666;font-size:0.8rem;margin-bottom:20px}
.stats-bar{display:flex;gap:12px;flex-wrap:wrap;margin-bottom:20px;color:#888;font-size:0.8rem}
.stat{background:#1a1a1a;border:1px solid #2a2a2a;padding:4px 10px;border-radius:3px}
.stat span{color:#aaa}
.search-row{display:flex;gap:8px;margin-bottom:24px}
#query{flex:1;background:#1a1a1a;border:1px solid #333;color:#e0e0e0;padding:10px 14px;
  font-family:monospace;font-size:1rem;border-radius:3px;outline:none}
#query:focus{border-color:#7eb8f7}
button{background:#1e3a5f;color:#7eb8f7;border:1px solid #2d5a8e;padding:10px 20px;
  font-family:monospace;font-size:0.9rem;cursor:pointer;border-radius:3px;white-space:nowrap}
button:hover{background:#2d5a8e}
.agents{display:grid;grid-template-columns:repeat(5,1fr);gap:10px;margin-bottom:24px}
@media(max-width:900px){.agents{grid-template-columns:repeat(2,1fr)}}
.agent-card{background:#111;border:1px solid #222;border-radius:4px;overflow:hidden}
.agent-header{padding:8px 12px;font-size:0.75rem;font-weight:bold;letter-spacing:0.05em}
.l1 .agent-header{background:#1a1a2e;color:#9988ff}
.l2 .agent-header{background:#1a2a1a;color:#88cc88}
.l3 .agent-header{background:#2a1a1a;color:#ff9966}
.l4 .agent-header{background:#1a2a2a;color:#66cccc}
.l5 .agent-header{background:#2a2a1a;color:#cccc66}
.agent-body{padding:8px;min-height:60px;font-size:0.72rem}
.spinner{color:#555;animation:pulse 1s ease-in-out infinite}
@keyframes pulse{0%,100%{opacity:0.4}50%{opacity:1}}
.result-row{padding:4px 0;border-bottom:1px solid #1c1c1c;line-height:1.4}
.result-row:last-child{border-bottom:none}
.score{color:#555;font-size:0.7rem}
.content{color:#ccc;overflow:hidden;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical}
.empty{color:#444;font-style:italic}
.err{color:#cc4444;font-size:0.75rem}
.merged-section{margin-top:8px}
.merged-section h2{font-size:0.85rem;color:#888;margin-bottom:10px;text-transform:uppercase;letter-spacing:0.1em}
table{width:100%;border-collapse:collapse;font-size:0.8rem}
thead th{text-align:left;color:#555;padding:6px 8px;border-bottom:1px solid #222;font-weight:normal}
tbody tr:hover{background:#161616}
td{padding:6px 8px;border-bottom:1px solid #1a1a1a;vertical-align:top}
.badge{display:inline-block;padding:1px 6px;border-radius:2px;font-size:0.7rem;font-weight:bold}
.badge-1{background:#1a1a2e;color:#9988ff}
.badge-2{background:#1a2a1a;color:#88cc88}
.badge-3{background:#2a1a1a;color:#ff9966}
.badge-4{background:#1a2a2a;color:#66cccc}
.badge-5{background:#2a2a1a;color:#cccc66}
.content-cell{max-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:#ccc}
.id-cell{color:#555;font-size:0.7rem;white-space:nowrap}
.score-cell{color:#666;text-align:right;white-space:nowrap}
.hidden{display:none}
.status{color:#555;font-size:0.75rem;margin-bottom:16px;min-height:1em}
</style>
</head>
<body>
<h1>lth — memory search</h1>
<p class="subtitle">parallel layer agents · composite scoring</p>

<div class="stats-bar" id="stats-bar">
  <div class="stat">loading stats...</div>
</div>

<div class="search-row">
  <input id="query" type="text" placeholder="search memories..." autocomplete="off" autofocus>
  <button onclick="runSearch()">search</button>
</div>

<div class="status" id="status"></div>

<div class="agents hidden" id="agents">
  <div class="agent-card l1" id="card-1">
    <div class="agent-header">L1 — identity</div>
    <div class="agent-body" id="body-1"><span class="spinner">···</span></div>
  </div>
  <div class="agent-card l2" id="card-2">
    <div class="agent-header">L2 — rules</div>
    <div class="agent-body" id="body-2"><span class="spinner">···</span></div>
  </div>
  <div class="agent-card l3" id="card-3">
    <div class="agent-header">L3 — skills</div>
    <div class="agent-body" id="body-3"><span class="spinner">···</span></div>
  </div>
  <div class="agent-card l4" id="card-4">
    <div class="agent-header">L4 — situational</div>
    <div class="agent-body" id="body-4"><span class="spinner">···</span></div>
  </div>
  <div class="agent-card l5" id="card-5">
    <div class="agent-header">L5 — recent</div>
    <div class="agent-body" id="body-5"><span class="spinner">···</span></div>
  </div>
</div>

<div class="merged-section hidden" id="merged-section">
  <h2>top results — all layers</h2>
  <table>
    <thead>
      <tr>
        <th>L</th>
        <th>score</th>
        <th>id</th>
        <th>content</th>
      </tr>
    </thead>
    <tbody id="merged-body"></tbody>
  </table>
</div>

<script>
const LAYER_NAMES = {1:'identity',2:'rules',3:'skills',4:'situational',5:'recent'};

async function loadStats() {
  try {
    const r = await fetch('/api/stats');
    if (!r.ok) return;
    const s = await r.json();
    const bar = document.getElementById('stats-bar');
    const parts = [
      stat('memories', s.TotalMemories),
      stat('edges', s.TotalEdges),
    ];
    for (let l = 1; l <= 5; l++) {
      parts.push(stat('L'+l, s.ByLayer[l] || 0));
    }
    bar.innerHTML = parts.join('');
  } catch(_) {}
}

function stat(label, val) {
  return '<div class="stat"><span>'+label+'</span> '+val+'</div>';
}

async function runSearch() {
  const query = document.getElementById('query').value.trim();
  if (!query) return;

  document.getElementById('agents').classList.remove('hidden');
  document.getElementById('merged-section').classList.add('hidden');
  document.getElementById('merged-body').innerHTML = '';
  document.getElementById('status').textContent = 'spawning layer agents...';

  // Reset all agent cards to spinner state
  for (let l = 1; l <= 5; l++) {
    document.getElementById('body-'+l).innerHTML = '<span class="spinner">···</span>';
  }

  const t0 = Date.now();
  const allResults = [];

  // Fire one fetch per layer — the multi-agent fan-out
  const layerFetches = [1,2,3,4,5].map(layer =>
    search(query, [layer], 8)
      .then(results => {
        renderLayerCard(layer, results);
        allResults.push(...results);
      })
      .catch(err => renderLayerError(layer, err))
  );

  // Also fire a merged search across all layers for the top-results table
  const mergedFetch = search(query, [], 20)
    .then(results => renderMerged(results))
    .catch(() => {});

  await Promise.all([...layerFetches, mergedFetch]);

  const elapsed = ((Date.now() - t0) / 1000).toFixed(2);
  document.getElementById('status').textContent =
    'completed in ' + elapsed + 's';
}

async function search(query, layers, topK) {
  const r = await fetch('/api/search', {
    method: 'POST',
    headers: {'Content-Type':'application/json'},
    body: JSON.stringify({query, layers, topK}),
  });
  if (!r.ok) throw new Error('HTTP '+r.status);
  return r.json();
}

function renderLayerCard(layer, results) {
  const body = document.getElementById('body-'+layer);
  if (!results || results.length === 0) {
    body.innerHTML = '<span class="empty">no results</span>';
    return;
  }
  body.innerHTML = results.map(r => {
    const content = (r.Content||'').replace(/</g,'&lt;').replace(/>/g,'&gt;');
    const preview = content.length > 120 ? content.slice(0,117)+'...' : content;
    return '<div class="result-row">' +
      '<div class="score">'+r.Score.toFixed(3)+'</div>' +
      '<div class="content">'+preview+'</div>' +
      '</div>';
  }).join('');
}

function renderLayerError(layer, err) {
  document.getElementById('body-'+layer).innerHTML =
    '<span class="err">error: '+err.message+'</span>';
}

function renderMerged(results) {
  if (!results || results.length === 0) return;
  const tbody = document.getElementById('merged-body');
  tbody.innerHTML = results.map(r => {
    const content = (r.Content||'').replace(/</g,'&lt;').replace(/>/g,'&gt;');
    const shortID = (r.ID||'').slice(0,8);
    return '<tr>' +
      '<td><span class="badge badge-'+r.Layer+'">L'+r.Layer+'</span></td>' +
      '<td class="score-cell">'+r.Score.toFixed(3)+'</td>' +
      '<td class="id-cell">'+shortID+'</td>' +
      '<td class="content-cell">'+content+'</td>' +
      '</tr>';
  }).join('');
  document.getElementById('merged-section').classList.remove('hidden');
}

document.getElementById('query').addEventListener('keydown', e => {
  if (e.key === 'Enter') runSearch();
});

loadStats();
</script>
</body>
</html>`
