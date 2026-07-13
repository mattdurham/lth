// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package traces

type otlpAttribute struct {
	Key   string       `json:"key"`
	Value otlpAnyValue `json:"value"`
}
