// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package traces

type otlpAnyValue struct {
	StringValue *string `json:"stringValue,omitempty"`
	IntValue    *int64  `json:"intValue,string,omitempty"`
	BoolValue   *bool   `json:"boolValue,omitempty"`
}
