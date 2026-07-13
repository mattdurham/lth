// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package traces

type otlpRequest struct {
	ResourceSpans []otlpResourceSpan `json:"resourceSpans"`
}
