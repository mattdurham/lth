// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

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
