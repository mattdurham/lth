// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package traces

type otlpResourceSpan struct {
	Resource   otlpResource    `json:"resource"`
	ScopeSpans []otlpScopeSpan `json:"scopeSpans"`
}
