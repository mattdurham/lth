// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/mattdurham/lth/pkg/lth"
)

// webChatHistoryItem is one turn of conversation, round-tripped through the browser.
type webChatHistoryItem struct {
	User      string `json:"user"`
	Assistant string `json:"assistant"`
}

// webChatRequest is the JSON body for POST /chat.
type webChatRequest struct {
	Message string               `json:"message"`
	History []webChatHistoryItem `json:"history"`
	Store   bool                 `json:"store"`
	Project string               `json:"project"` // optional; boosts memories via FilterAttrs, same as --attr project=X
}

// webChatResponse is the JSON body returned from POST /chat.
type webChatResponse struct {
	Reply   string               `json:"reply"`
	History []webChatHistoryItem `json:"history"`
}

// doChatFn is the function used to perform a chat turn.
// Replaced in tests to avoid live LLM calls. globalLLM() is called inside
// the default so tests that replace doChatFn never trigger it.
var doChatFn = func(ctx context.Context, client *lth.Client, question string, history []chatTurn, filterAttrs map[string]string) (string, error) {
	return doChat(ctx, client, globalLLM(), question, history, filterAttrs)
}

func handleWebChatPage(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, chatHTML)
}

func handleWebChatAPI(w http.ResponseWriter, r *http.Request, client *lth.Client) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req webChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.Message == "" {
		http.Error(w, "message required", http.StatusBadRequest)
		return
	}

	history := make([]chatTurn, len(req.History))
	for i, h := range req.History {
		history[i] = chatTurn{user: h.User, assistant: h.Assistant}
	}

	var filterAttrs map[string]string
	if req.Project != "" {
		filterAttrs = map[string]string{"project": req.Project}
	}

	// chatLayers and chatTopK are read-only after startup — safe for concurrent use.
	reply, err := doChatFn(r.Context(), client, req.Message, history, filterAttrs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if req.Store {
		content := fmt.Sprintf("Q: %s\nA: %s", req.Message, reply)
		_, _ = client.Store(r.Context(), 5, content, map[string]string{"source": "chat"})
	}

	updated := append(req.History, webChatHistoryItem{User: req.Message, Assistant: reply})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(webChatResponse{Reply: reply, History: updated})
}

const chatHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>lth chat</title>
  <script src="https://cdn.tailwindcss.com"></script>
  <script src="https://cdn.jsdelivr.net/npm/marked/marked.min.js"></script>
  <style>
    .prose-chat p { margin: 0.4em 0; }
    .prose-chat p:first-child { margin-top: 0; }
    .prose-chat p:last-child { margin-bottom: 0; }
    .prose-chat ul, .prose-chat ol { margin: 0.4em 0 0.4em 1.2em; }
    .prose-chat li { margin: 0.15em 0; }
    .prose-chat code { background: rgba(255,255,255,0.08); padding: 0.1em 0.35em; border-radius: 3px; font-size: 0.9em; }
    .prose-chat pre { background: rgba(255,255,255,0.06); border: 1px solid rgba(255,255,255,0.1); border-radius: 6px; padding: 0.75em 1em; overflow-x: auto; margin: 0.5em 0; }
    .prose-chat pre code { background: none; padding: 0; }
    .prose-chat h1, .prose-chat h2, .prose-chat h3 { font-weight: 600; margin: 0.6em 0 0.3em; }
    .prose-chat strong { font-weight: 600; }
    .prose-chat a { color: #818cf8; text-decoration: underline; }
    .prose-chat hr { border-color: rgba(255,255,255,0.1); margin: 0.5em 0; }
    .prose-chat blockquote { border-left: 3px solid rgba(255,255,255,0.2); padding-left: 0.75em; color: rgba(255,255,255,0.6); }
  </style>
</head>
<body class="bg-gray-950 text-gray-100 min-h-screen flex flex-col font-mono">
  <div class="border-b border-gray-800 px-6 py-3 flex items-center justify-between">
    <a href="/" class="text-gray-400 hover:text-gray-200 text-sm transition-colors">&larr; search</a>
    <span class="text-indigo-400 font-bold">lth chat</span>
    <div class="flex items-center gap-4 text-sm text-gray-400">
      <label class="flex items-center gap-2">
        Project:
        <select id="project" title="Boosts matching results; other projects still appear."
          class="bg-gray-800 border border-gray-700 rounded px-2 py-1 text-gray-100 focus:outline-none focus:border-indigo-500">
          <option value="">All</option>
        </select>
      </label>
      <label class="flex items-center gap-2">
        <input type="checkbox" id="storeToggle" checked> store as L5
      </label>
    </div>
  </div>
  <div id="messages" class="flex-1 overflow-y-auto px-6 py-4 space-y-4 max-w-4xl mx-auto w-full"></div>
  <div id="status" class="text-gray-500 text-xs text-center py-1"></div>
  <div class="border-t border-gray-800 px-6 py-4 max-w-4xl mx-auto w-full">
    <div class="flex gap-2">
      <textarea id="input" rows="2" placeholder="ask a question..."
        class="flex-1 bg-gray-800 border border-gray-700 rounded px-4 py-2 text-gray-100 placeholder-gray-500 focus:outline-none focus:border-indigo-500 resize-none"
        onkeydown="if(event.key==='Enter'&&!event.shiftKey){event.preventDefault();send()}">
      </textarea>
      <button id="sendBtn" onclick="send()"
        class="bg-indigo-600 hover:bg-indigo-500 px-6 py-2 rounded font-semibold transition-colors self-end">
        Send
      </button>
    </div>
  </div>
  <script>
    let history = [];

    function esc(s) {
      return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
    }

    function addBubble(role, text) {
      const msgs = document.getElementById('messages');
      const div = document.createElement('div');
      div.className = role === 'user' ? 'flex justify-end' : 'flex justify-start';
      const inner = document.createElement('div');
      inner.className = role === 'user'
        ? 'bg-indigo-900 border border-indigo-800 rounded-lg px-4 py-2 max-w-2xl text-sm leading-relaxed whitespace-pre-wrap'
        : 'prose-chat bg-gray-900 border border-gray-800 rounded-lg px-4 py-2 max-w-2xl text-sm leading-relaxed text-gray-200 relative';
      if (role === 'user') {
        inner.textContent = text;
      } else {
        inner.innerHTML = marked.parse(text);
        const btn = document.createElement('button');
        btn.className = 'absolute top-2 right-2 bg-gray-700 hover:bg-gray-600 text-gray-300 text-xs px-2 py-1 rounded';
        btn.textContent = 'copy';
        btn.onclick = () => {
          navigator.clipboard.writeText(text).then(() => {
            btn.textContent = 'copied!';
            setTimeout(() => btn.textContent = 'copy', 1500);
          });
        };
        inner.appendChild(btn);
      }
      div.appendChild(inner);
      msgs.appendChild(div);
      msgs.scrollTop = msgs.scrollHeight;
      return div;
    }

    async function send() {
      const input = document.getElementById('input');
      const btn = document.getElementById('sendBtn');
      const status = document.getElementById('status');
      const store = document.getElementById('storeToggle').checked;
      const project = document.getElementById('project').value;

      const msg = input.value.trim();
      if (!msg) return;

      input.disabled = true;
      btn.disabled = true;
      status.textContent = 'thinking...';
      const userBubble = addBubble('user', msg);

      try {
        const res = await fetch('/chat', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({message: msg, history: history, store: store, project: project}),
        });
        if (!res.ok) {
          const text = await res.text();
          status.textContent = 'error: ' + text.trim();
          userBubble.remove();
          return;
        }
        const data = await res.json();
        history = data.history;
        addBubble('assistant', data.reply);
        input.value = '';
        status.textContent = '';
      } catch(e) {
        status.textContent = 'error: ' + e.message;
        userBubble.remove();
      } finally {
        input.disabled = false;
        btn.disabled = false;
        input.focus();
      }
    }

    async function loadProjects() {
      try {
        const res = await fetch('/projects');
        const projects = await res.json();
        const select = document.getElementById('project');
        for (const p of projects) {
          const opt = document.createElement('option');
          opt.value = p;
          opt.textContent = p;
          select.appendChild(opt);
        }
      } catch (e) {
        // Non-fatal: dropdown just stays at "All".
      }
    }

    loadProjects();
    document.getElementById('input').focus();
  </script>
</body>
</html>`
