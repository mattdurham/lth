// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package dataset

import "net/http"

type HFClient struct {
	httpClient *http.Client
	baseURL    string
}
