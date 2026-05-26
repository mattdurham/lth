// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mattdurham/lth/internal/blobstore"
	"github.com/mattdurham/lth/internal/parquet"
)

// Server is the lth-server HTTP server.
type Server struct {
	cfg    ServerConfig
	store  blobstore.BlobStore
	writer *parquet.Writer
	reader *parquet.Reader
	srv    *http.Server
}

// newServer creates a Server from cfg, opening the BlobStore.
func newServer(ctx context.Context, cfg ServerConfig) (*Server, error) {
	store, err := buildStore(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("build store: %w", err)
	}
	return &Server{
		cfg:    cfg,
		store:  store,
		writer: parquet.NewWriter(),
		reader: parquet.NewReader(),
	}, nil
}

func buildStore(ctx context.Context, cfg ServerConfig) (blobstore.BlobStore, error) {
	switch strings.ToLower(cfg.Storage.Provider) {
	case "local":
		dir := expandHome(cfg.Storage.LocalDir)
		return blobstore.NewLocalStore(dir)
	case "s3":
		return blobstore.NewS3Store(ctx, blobstore.S3Config{
			Endpoint:        cfg.Storage.Endpoint,
			Bucket:          cfg.Storage.Bucket,
			Region:          cfg.Storage.Region,
			AccessKeyID:     cfg.Storage.AccessKeyID,
			SecretAccessKey: cfg.Storage.SecretAccessKey,
			UseSSL:          cfg.Storage.UseSSL,
		})
	default:
		return nil, fmt.Errorf("unknown storage provider %q (use \"local\" or \"s3\")", cfg.Storage.Provider)
	}
}

func expandHome(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[2:])
}
func (s *Server) buildMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.Handle("/v1/sync/push", instrumentHandler("push", pushRequests, &PushHandler{store: s.store, writer: s.writer, cfg: s.cfg}))
	mux.Handle("/v1/sync/pull", instrumentHandler("pull", pullRequests, &PullHandler{store: s.store, reader: s.reader}))
	mux.Handle("/v1/observations", instrumentHandler("observe", obsRequests, &ObserveHandler{store:

	// Start begins serving HTTP. Blocks until ctx is canceled.
	s.store, writer: s.writer}))
	mux.Handle("/metrics", metricsHandler())
	return mux
}

func (s *Server) Start(ctx context.Context) error {
	s.srv = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.cfg.Port),
		Handler:      http.MaxBytesHandler(s.buildMux(), 100*1024*1024),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() { errCh <- s.srv.ListenAndServe() }()
	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.srv.Shutdown(shutCtx) //nolint:contextcheck
	case err := <-errCh:
		return err
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintln(w, "ok")
}
