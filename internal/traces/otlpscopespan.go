package traces

type otlpScopeSpan struct {
	Spans []otlpSpan `json:"spans"`
}
