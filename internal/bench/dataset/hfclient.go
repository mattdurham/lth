package dataset

import "net/http"

type HFClient struct {
	httpClient *http.Client
	baseURL    string
}
