package vector

import (
	"net/http"
	"sync/atomic"
)

type OllamaEmbedder struct {
	baseURL string
	model   string
	client  *http.Client
	dims    atomic.Int64
}
