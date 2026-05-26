package traces

type otlpRequest struct {
	ResourceSpans []otlpResourceSpan `json:"resourceSpans"`
}
