// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/mattdurham/lth/pkg/lth"
	"github.com/spf13/cobra"
)

var uiPort int

var uiCmd = &cobra.Command{
	Use:   "ui",
	Short: "Start a local web UI for querying memories",
	RunE:  runUI,
}

func init() {
	uiCmd.Flags().IntVar(&uiPort, "port", 8765, "port to listen on")
	rootCmd.AddCommand(uiCmd)
}

// memSearcher is satisfied by *lth.Client and *memory.MemoryStore.
type memSearcher interface {
	Search(ctx context.Context, req *lth.SearchRequest) ([]*lth.SearchResult, error)
}

// startUIServer serves the web UI on the given port until ctx is cancelled.
// client may be nil — when nil, the /chat route returns 503.
func startUIServer(ctx context.Context, s memSearcher, client *lth.Client, port int) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleUIIndex)
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		handleUISearch(w, r, s)
	})
	mux.HandleFunc("/chat", func(w http.ResponseWriter, r *http.Request) {
		if client == nil {
			http.Error(w, "chat unavailable in daemon mode", http.StatusServiceUnavailable)
			return
		}
		if r.Method == http.MethodPost {
			handleWebChatAPI(w, r, client)
			return
		}
		handleWebChatPage(w, r)
	})
	srv := &http.Server{Addr: fmt.Sprintf(":%d", port), Handler: mux}
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Printf("lth UI error: %v\n", err)
	}
}

func runUI(cmd *cobra.Command, _ []string) error {
	client, err := lth.NewClient(globalCfg)
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}
	defer client.Close() //nolint:errcheck

	fmt.Printf("lth UI running at http://localhost:%d\n", uiPort)
	startUIServer(cmd.Context(), client, client, uiPort)
	return nil
}

func handleUISearch(w http.ResponseWriter, r *http.Request, client memSearcher) {
	q := r.URL.Query().Get("q")
	if q == "" {
		http.Error(w, "q required", http.StatusBadRequest)
		return
	}

	topK := 20
	if t := r.URL.Query().Get("top"); t != "" {
		if n, err := strconv.Atoi(t); err == nil && n > 0 {
			topK = n
		}
	}

	var layers []int
	if l := r.URL.Query().Get("layers"); l != "" {
		for _, s := range strings.Split(l, ",") {
			if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
				layers = append(layers, n)
			}
		}
	}

	expand := r.URL.Query().Get("expand") == "1"

	req := &lth.SearchRequest{
		Query:  q,
		Layers: layers,
		TopK:   topK,
		Expand: expand,
	}

	results, err := client.Search(context.Background(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	type resultItem struct {
		ID         string  `json:"id"`
		Layer      int     `json:"layer"`
		Content    string  `json:"content"`
		Score      float32 `json:"score"`
		Vector     float32 `json:"vector_score"`
		Time       float32 `json:"time_score"`
		Importance float32 `json:"importance_score"`
		Valence    float32 `json:"valence"`
		Source     string  `json:"source"`
	}

	items := make([]resultItem, 0, len(results))
	for _, r := range results {
		items = append(items, resultItem{
			ID:         r.ID,
			Layer:      r.Layer,
			Content:    r.Content,
			Score:      r.Score,
			Vector:     r.VectorScore,
			Time:       r.TimeScore,
			Importance: r.ImportanceScore,
			Valence:    r.Valence,
			Source:     r.Source,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(items)
}

func handleUIIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, uiHTML)
}

const uiHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>lth memory search</title>
<script src="https://cdn.tailwindcss.com"></script>
</head>
<body class="bg-gray-950 text-gray-100 min-h-screen p-6 font-mono">
<div class="max-w-4xl mx-auto">
  <div class="flex items-center justify-between mb-6">
    <h1 class="text-2xl font-bold text-indigo-400">lth memory search</h1>
    <a href="/chat" class="text-indigo-400 hover:text-indigo-300 text-sm transition-colors">chat &rarr;</a>
  </div>

  <div class="flex gap-2 mb-4">
    <input id="q" type="text" placeholder="search memories..."
      class="flex-1 bg-gray-800 border border-gray-700 rounded px-4 py-2 text-gray-100 placeholder-gray-500 focus:outline-none focus:border-indigo-500"
      onkeydown="if(event.key==='Enter') search()">
    <button onclick="search()"
      class="bg-indigo-600 hover:bg-indigo-500 px-6 py-2 rounded font-semibold transition-colors">
      Search
    </button>
  </div>

  <div class="flex flex-wrap gap-4 mb-6 text-sm">
    <div class="flex items-center gap-2">
      <span class="text-gray-400">Layers:</span>
      <label class="flex items-center gap-1"><input type="checkbox" class="layer" value="1"> L1</label>
      <label class="flex items-center gap-1"><input type="checkbox" class="layer" value="2"> L2</label>
      <label class="flex items-center gap-1"><input type="checkbox" class="layer" value="3"> L3</label>
      <label class="flex items-center gap-1"><input type="checkbox" class="layer" value="4" checked> L4</label>
      <label class="flex items-center gap-1"><input type="checkbox" class="layer" value="5"> L5</label>
    </div>
    <div class="flex items-center gap-2">
      <span class="text-gray-400">Top:</span>
      <input id="topk" type="number" value="20" min="1" max="100"
        class="w-16 bg-gray-800 border border-gray-700 rounded px-2 py-1 text-gray-100 focus:outline-none focus:border-indigo-500">
    </div>
    <label class="flex items-center gap-2">
      <input type="checkbox" id="expand"> <span class="text-gray-400">Graph expand</span>
    </label>
  </div>

  <div id="status" class="text-gray-500 text-sm mb-4"></div>
  <div id="results" class="space-y-3"></div>
</div>

<script>
const layerColors = {1:'text-purple-400',2:'text-blue-400',3:'text-green-400',4:'text-yellow-400',5:'text-orange-400'};
const layerNames = {1:'core',2:'principles',3:'knowledge',4:'workspace',5:'observations'};

async function search() {
  const q = document.getElementById('q').value.trim();
  if (!q) return;

  const layers = [...document.querySelectorAll('.layer:checked')].map(e => e.value).join(',');
  const topk = document.getElementById('topk').value;
  const expand = document.getElementById('expand').checked ? '1' : '0';

  document.getElementById('status').textContent = 'searching...';
  document.getElementById('results').innerHTML = '';

  const params = new URLSearchParams({q, top: topk, expand});
  if (layers) params.set('layers', layers);

  try {
    const res = await fetch('/search?' + params);
    const data = await res.json();

    document.getElementById('status').textContent =
      data.length ? data.length + ' result' + (data.length !== 1 ? 's' : '') : 'no results';

    document.getElementById('results').innerHTML = data.map(r => {
      const lc = layerColors[r.layer] || 'text-gray-400';
      const ln = layerNames[r.layer] || 'L' + r.layer;
      const score = (r.score * 100).toFixed(1);
      const vec = (r.vector_score * 100).toFixed(1);
      const content = r.content.length > 400 ? r.content.slice(0, 400) + '…' : r.content;
      const source = r.source ? '<span class="text-gray-500">' + esc(r.source) + '</span>' : '';
      return ` + "`" + `
        <div class="bg-gray-900 border border-gray-800 rounded-lg p-4 hover:border-gray-600 transition-colors">
          <div class="flex items-center justify-between mb-2">
            <div class="flex items-center gap-3">
              <span class="${lc} text-xs font-semibold uppercase">${ln}</span>
              ${source}
            </div>
            <div class="flex gap-3 text-xs text-gray-500">
              <span title="composite score">⭐ ${score}%</span>
              <span title="vector similarity">🔍 ${vec}%</span>
              <span title="valence" class="${r.valence > 0.1 ? 'text-green-500' : r.valence < -0.1 ? 'text-red-500' : 'text-gray-500'}">${r.valence >= 0 ? '+' : ''}${r.valence.toFixed(2)}</span>
            </div>
          </div>
          <p class="text-gray-200 text-sm leading-relaxed whitespace-pre-wrap">${esc(content)}</p>
          <div class="mt-2 text-xs text-gray-600">${r.id.slice(0, 8)}</div>
        </div>` + "`" + `;
    }).join('');
  } catch (e) {
    document.getElementById('status').textContent = 'error: ' + e.message;
  }
}

function esc(s) {
  return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
}

document.getElementById('q').focus();
</script>
</body>
</html>`
