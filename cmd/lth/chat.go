package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/mattdurham/lth/internal/llm"
	"github.com/mattdurham/lth/pkg/lth"
	"github.com/spf13/cobra"
)

var (
	chatTopK        int
	chatLayers      []int
	chatStore       bool
	chatFilterAttrs []string
)

var chatCmd = &cobra.Command{
	Use:   "chat [question]",
	Short: "Chat with your knowledge base",
	Long:  "Ask questions answered by your memories. No argument = interactive REPL.",
	RunE:  runChat,
}

func init() {
	chatCmd.Flags().IntVar(&chatTopK, "top", 50, "memories to retrieve per turn")
	chatCmd.Flags().IntSliceVar(&chatLayers, "layers", []int{1, 2, 3, 4, 5}, "layers to search")
	chatCmd.Flags().BoolVar(&chatStore, "store", true, "store each exchange as L5")
	chatCmd.Flags().StringArrayVar(&chatFilterAttrs, "attr", nil, "boost memories matching attribute key=value (e.g. --attr project=grafana/tempo)")
	rootCmd.AddCommand(chatCmd)
}

type chatTurn struct {
	user      string
	assistant string
}

const chatSystemPrompt = `You are a personal AI assistant with access to the user's knowledge base — their memories, notes, observations, and work history.

You will receive a list of candidate memories retrieved by vector search. Some will be highly relevant, others tangentially related or noise. Your job is to:
1. Identify which memories are actually useful for answering the question
2. Synthesize a specific, accurate answer from those relevant memories
3. Ignore memories that are not pertinent

Be specific — cite details, names, metrics, and decisions from the memories. If the relevant memories don't contain enough to answer confidently, say so clearly and share what is available.

Do not fabricate information not present in the memories.`

func runChat(cmd *cobra.Command, args []string) error {
	client, err := lth.NewClient(globalCfg)
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}
	defer client.Close() //nolint:errcheck

	l := globalLLM()
	ctx := cmd.Context()

	// One-shot mode
	if len(args) > 0 {
		answer, err := doChat(ctx, client, l, strings.Join(args, " "), nil)
		if err != nil {
			return err
		}
		fmt.Println(answer)
		return nil
	}

	// Interactive REPL
	var history []chatTurn
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Fprintln(os.Stderr, "lth chat — type your question, 'exit' to quit")
	fmt.Fprint(os.Stderr, "\n> ")
	for scanner.Scan() {
		q := strings.TrimSpace(scanner.Text())
		if q == "" {
			fmt.Fprint(os.Stderr, "> ")
			continue
		}
		if q == "exit" || q == "quit" || q == "/exit" {
			break
		}

		answer, err := doChat(ctx, client, l, q, history)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			fmt.Fprint(os.Stderr, "> ")
			continue
		}

		fmt.Printf("\n%s\n", answer)
		history = append(history, chatTurn{user: q, assistant: answer})

		if chatStore {
			content := fmt.Sprintf("Q: %s\nA: %s", q, answer)
			_, _ = client.Store(ctx, 5, content, map[string]string{"source": "chat"})
		}

		fmt.Fprint(os.Stderr, "\n> ")
	}
	return nil
}

var chatTools = []llm.Tool{
	{
		Name:        "search",
		Description: "Search the knowledge base for memories relevant to a query. Use this to find more context on any topic.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"query":  {"type": "string",  "description": "Search query"},
				"layers": {"type": "array", "items": {"type": "integer"}, "description": "Layers to search (1=core,2=principles,3=knowledge,4=workspace,5=observations). Omit for all."},
				"top":    {"type": "integer", "description": "Number of results (default 20)"}
			},
			"required": ["query"]
		}`),
	},
	{
		Name:        "get_memory",
		Description: "Retrieve the full content of a specific memory by its ID.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"id": {"type": "string", "description": "Memory ID or unique prefix"}
			},
			"required": ["id"]
		}`),
	},
	{
		Name:        "get_neighbors",
		Description: "Get graph neighbors of a memory — related memories connected by edges. Use this to explore connected context.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"id":    {"type": "string",  "description": "Memory ID"},
				"depth": {"type": "integer", "description": "Traversal depth 1-3 (default 1)"}
			},
			"required": ["id"]
		}`),
	},
}

func doChat(ctx context.Context, client *lth.Client, l llm.LLM, question string, history []chatTurn) (string, error) {
	// Seed with initial search results
	results, err := client.Search(ctx, &lth.SearchRequest{
		Query:       question,
		Layers:      chatLayers,
		TopK:        chatTopK,
		Expand:      true,
		FilterAttrs: parseAttrs(chatFilterAttrs),
	})
	if err != nil {
		return "", fmt.Errorf("search: %w", err)
	}

	// Build user message: seed context + conversation history + question
	var sb strings.Builder
	sb.WriteString("Here are memory search results for your question (ordered by relevance). Use the tools to search further or get more detail if needed.\n\n")
	sb.WriteString("---\nInitial context:\n\n")
	for i, r := range results {
		fmt.Fprintf(&sb, "[%d] L%d", i+1, r.Layer)
		if r.Source != "" && r.Source != "server" {
			fmt.Fprintf(&sb, " (%s)", r.Source)
		}
		fmt.Fprintf(&sb, " id=%s\n%s\n\n", r.ID[:8], r.Content)
	}

	if len(history) > 0 {
		sb.WriteString("---\nConversation so far:\n\n")
		start := 0
		if len(history) > 6 {
			start = len(history) - 6
		}
		for _, h := range history[start:] {
			fmt.Fprintf(&sb, "Human: %s\nAssistant: %s\n\n", h.user, h.assistant)
		}
	}

	fmt.Fprintf(&sb, "---\nQuestion: %s", question)

	// Tool executor
	executor := func(ctx context.Context, name string, input json.RawMessage) (string, error) {
		switch name {
		case "search":
			var args struct {
				Query  string `json:"query"`
				Layers []int  `json:"layers"`
				Top    int    `json:"top"`
			}
			if err := json.Unmarshal(input, &args); err != nil {
				return "", err
			}
			if args.Top == 0 {
				args.Top = 20
			}
			if len(args.Layers) == 0 {
				args.Layers = chatLayers
			}
			res, err := client.Search(ctx, &lth.SearchRequest{
				Query: args.Query, Layers: args.Layers, TopK: args.Top,
			})
			if err != nil {
				return "", err
			}
			var out strings.Builder
			for i, r := range res {
				fmt.Fprintf(&out, "[%d] L%d id=%s score=%.3f\n%s\n\n", i+1, r.Layer, r.ID[:8], r.Score, r.Content)
			}
			return out.String(), nil

		case "get_memory":
			var args struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(input, &args); err != nil {
				return "", err
			}
			m, err := client.Get(ctx, args.ID)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("L%d [%s]\n%s", m.Layer, m.ID, m.Content), nil

		case "get_neighbors":
			var args struct {
				ID    string `json:"id"`
				Depth int    `json:"depth"`
			}
			if err := json.Unmarshal(input, &args); err != nil {
				return "", err
			}
			if args.Depth == 0 {
				args.Depth = 1
			}
			edges, err := client.GraphNeighbors(ctx, args.ID, args.Depth)
			if err != nil {
				return "", err
			}
			var out strings.Builder
			fmt.Fprintf(&out, "%d neighbors:\n", len(edges))
			for _, e := range edges {
				fmt.Fprintf(&out, "  %s -[%s]-> %s (weight=%.2f)\n", e.FromID[:8], e.EdgeType, e.ToID[:8], e.Weight)
			}
			return out.String(), nil
		}
		return "", fmt.Errorf("unknown tool: %s", name)
	}

	// Use agentic loop if LLM supports it
	if al, ok := l.(*llm.AnthropicLLM); ok {
		answer, err := al.CompleteWithTools(ctx, chatSystemPrompt, sb.String(), chatTools, executor)
		if err != nil {
			return "", fmt.Errorf("llm: %w", err)
		}
		return strings.TrimSpace(answer), nil
	}

	// Fallback: simple completion without tools
	var prompt strings.Builder
	prompt.WriteString(chatSystemPrompt)
	prompt.WriteString("\n\n")
	prompt.WriteString(sb.String())
	answer, err := l.Complete(ctx, prompt.String())
	if err != nil {
		return "", fmt.Errorf("llm: %w", err)
	}
	return strings.TrimSpace(answer), nil
}

// globalLLM returns an LLM instance using the global config.
func globalLLM() llm.LLM {
	return llm.New(globalCfg)
}

