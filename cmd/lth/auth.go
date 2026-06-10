// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/mattdurham/lth/internal/llm/anthropicauth"
	"github.com/spf13/cobra"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage Anthropic OAuth (claude.ai Pro/Max) authentication",
	Long: `Authenticate lth against an Anthropic Pro/Max subscription using the
same OAuth flow as Claude Code, so the Anthropic LLM provider can call the
Messages API without an x-api-key.

Run ` + "`lth auth login`" + ` then set ` + "`llm.auth_mode: oauth`" + ` in ~/.lth/config.yaml.`,
}

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Run the claude.ai OAuth PKCE flow and store credentials",
	RunE: func(cmd *cobra.Command, _ []string) error {
		path, err := credentialsPath()
		if err != nil {
			return err
		}

		ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt)
		defer cancel()
		ctx, timeoutCancel := context.WithTimeout(ctx, 5*time.Minute)
		defer timeoutCancel()

		fmt.Fprintln(os.Stderr, "Starting Anthropic OAuth flow on http://127.0.0.1:53692 ...")
		creds, err := anthropicauth.Login(ctx, anthropicauth.LoginOptions{
			OpenBrowser: true,
			OnAuthURL: func(u string) {
				fmt.Fprintln(os.Stderr, "If your browser did not open, visit:")
				fmt.Fprintln(os.Stderr, "  "+u)
			},
		})
		if err != nil {
			return fmt.Errorf("oauth login: %w", err)
		}
		if err := anthropicauth.Save(path, creds); err != nil {
			return err
		}
		if flagJSON {
			return json.NewEncoder(os.Stdout).Encode(map[string]any{
				"status":     "ok",
				"path":       path,
				"expires_ms": creds.ExpiresMs,
			})
		}
		fmt.Fprintf(os.Stderr, "Saved credentials to %s\n", path)
		fmt.Fprintln(os.Stderr, "Set `llm.auth_mode: oauth` in ~/.lth/config.yaml to use them.")
		return nil
	},
}

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Delete stored Anthropic OAuth credentials",
	RunE: func(_ *cobra.Command, _ []string) error {
		path, err := credentialsPath()
		if err != nil {
			return err
		}
		if err := anthropicauth.Delete(path); err != nil {
			return err
		}
		if flagJSON {
			return json.NewEncoder(os.Stdout).Encode(map[string]any{"status": "ok", "path": path})
		}
		fmt.Fprintf(os.Stderr, "Deleted %s\n", path)
		return nil
	},
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show stored Anthropic OAuth credential status",
	RunE: func(_ *cobra.Command, _ []string) error {
		path, err := credentialsPath()
		if err != nil {
			return err
		}
		creds, err := anthropicauth.Load(path)
		if err != nil {
			if errors.Is(err, anthropicauth.ErrNoCredentials) {
				if flagJSON {
					return json.NewEncoder(os.Stdout).Encode(map[string]any{
						"status": "missing", "path": path,
					})
				}
				fmt.Fprintf(os.Stderr, "No credentials at %s\n", path)
				return nil
			}
			return err
		}
		expiresAt := time.UnixMilli(creds.ExpiresMs)
		valid := time.Now().Before(expiresAt)
		if flagJSON {
			return json.NewEncoder(os.Stdout).Encode(map[string]any{
				"status":     "ok",
				"path":       path,
				"expires_at": expiresAt.Format(time.RFC3339),
				"valid":      valid,
			})
		}
		fmt.Fprintf(os.Stderr, "Credentials: %s\n", path)
		fmt.Fprintf(os.Stderr, "Access token expires: %s (valid=%v)\n", expiresAt.Format(time.RFC3339), valid)
		return nil
	},
}

func credentialsPath() (string, error) {
	if globalCfg != nil && globalCfg.LLM.OAuthCredentialsPath != "" {
		return globalCfg.LLM.OAuthCredentialsPath, nil
	}
	return anthropicauth.DefaultPath()
}

func init() {
	authCmd.AddCommand(authLoginCmd, authLogoutCmd, authStatusCmd)
	rootCmd.AddCommand(authCmd)
}
