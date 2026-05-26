package traces

type otlpAnyValue struct {
	StringValue *string `json:"stringValue,omitempty"`
	IntValue    *int64  `json:"intValue,string,omitempty"`
	BoolValue   *bool   `json:"boolValue,omitempty"`
}
