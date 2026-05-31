package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/mattdurham/lth/internal/llm"
	"github.com/mattdurham/lth/pkg/lth"
	"github.com/spf13/cobra"
)

var (
	chatTopK   int
	chatLayers []int
	chatStore  bool
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

func doChat(ctx context.Context, client *lth.Client, l llm.LLM, question string, history []chatTurn) (string, error) {
	results, err := client.Search(ctx, &lth.SearchRequest{
		Query:  question,
		Layers: chatLayers,
		TopK:   chatTopK,
		Expand: true,
	})
	if err != nil {
		return "", fmt.Errorf("search: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(chatSystemPrompt)
	sb.WriteString("\n\n---\nKnowledge base context (ordered by relevance):\n\n")
	for i, r := range results {
		sb.WriteString(fmt.Sprintf("[%d] L%d", i+1, r.Layer))
		if r.Source != "" && r.Source != "server" {
			sb.WriteString(fmt.Sprintf(" (%s)", r.Source))
		}
		sb.WriteString(fmt.Sprintf(": %s\n\n", r.Content))
	}

	if len(history) > 0 {
		sb.WriteString("---\nConversation so far:\n\n")
		start := 0
		if len(history) > 6 {
			start = len(history) - 6
		}
		for _, h := range history[start:] {
			sb.WriteString(fmt.Sprintf("Human: %s\nAssistant: %s\n\n", h.user, h.assistant))
		}
	}

	sb.WriteString(fmt.Sprintf("---\nHuman: %s\nAssistant:", question))

	answer, err := l.Complete(ctx, sb.String())
	if err != nil {
		return "", fmt.Errorf("llm: %w", err)
	}

	return strings.TrimSpace(answer), nil
}

// globalLLM returns an LLM instance using the global config.
func globalLLM() llm.LLM {
	return llm.New(globalCfg)
}
