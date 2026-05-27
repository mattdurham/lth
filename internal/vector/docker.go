// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package vector

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mattdurham/lth/internal/config"
)

const containerName = "lth-embeddings"
const healthCheckInterval = 2 * time.Second
const healthCheckTimeout = 90 * time.Second // first run may download model

// EnsureEmbeddingServer starts the Docker container if the embedding server is unreachable.
// Returns nil if the server is already reachable or was started successfully.
// Returns an error if docker is unavailable or the container fails to start.
// No-op if cfg.Embedding.AutoDocker is false or provider is not "huggingface".
func EnsureEmbeddingServer(cfg *config.Config) error {
	if !cfg.Embedding.AutoDocker {
		return nil
	}
	if cfg.Embedding.Provider != "huggingface" {
		return nil
	}

	// 1. Server is already reachable — nothing to do.
	if pingEmbeddingServer(cfg.Embedding.BaseURL) == nil {
		return nil
	}

	// 2. Check docker is available.
	//nolint:gosec // G204: docker command uses config values, not user input
	if err := exec.Command("docker", "info").Run(); err != nil {
		return fmt.Errorf("embedding server unreachable and docker not available: %w", err)
	}

	// 3. Inspect existing container status.
	//nolint:gosec // G204: docker command uses config values, not user input
	out, _ := exec.Command("docker", "ps", "-a",
		"--filter", "name="+containerName,
		"--format", "{{.Status}}").Output()
	status := strings.TrimSpace(string(out))

	switch {
	case strings.HasPrefix(status, "Exited"):
		// Container exists but is stopped — restart it.
		//nolint:gosec // G204: docker command uses config values, not user input
		_ = exec.Command("docker", "start", containerName).Run()

	case status == "":
		// Container does not exist — create it.
		if err := runNewContainer(cfg); err != nil {
			return err
		}
		// Otherwise status indicates the container is already running; fall through to health check.
	}

	// 4. Wait for server to become ready.
	deadline := time.Now().Add(healthCheckTimeout)
	for time.Now().Before(deadline) {
		if pingEmbeddingServer(cfg.Embedding.BaseURL) == nil {
			return nil
		}
		time.Sleep(healthCheckInterval)
	}
	return fmt.Errorf("embedding server did not become ready within %s (model may still be downloading)", healthCheckTimeout)
}

// runNewContainer creates and starts a new HuggingFace TEI container.
func runNewContainer(cfg *config.Config) error {
	homeDir, _ := os.UserHomeDir()
	cacheDir := filepath.Join(homeDir, ".lth", "hf-cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return fmt.Errorf("create hf cache dir: %w", err)
	}

	port := fmt.Sprintf("%d:80", cfg.Embedding.DockerPort)

	//nolint:gosec // G204: docker command uses config values, not user input
	args := []string{
		"run", "-d",
		"--name", containerName,
		"-p", port,
		"-v", cacheDir + ":/data",
		cfg.Embedding.DockerImage,
		"--model-id", cfg.Embedding.Model,
		"--port", "80",
	}
	if cfg.Embedding.TrustRemoteCode {
		args = append(args, "--trust-remote-code")
	}
	//nolint:gosec // G204: docker command uses config values, not user input
	cmd := exec.Command("docker", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("start embedding container: %w\n%s", err, out)
	}
	return nil
}

// pingEmbeddingServer checks if the embedding server's /health endpoint is reachable.
func pingEmbeddingServer(baseURL string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/health", nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req) //nolint:gosec // G704: URL is from trusted config, not user input
	if err != nil {
		return err
	}
	resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check returned status %d", resp.StatusCode)
	}
	return nil
}
