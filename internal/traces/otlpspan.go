package traces

type otlpSpan struct {
	TraceID           string          `json:"traceId"`
	SpanID            string          `json:"spanId"`
	ParentSpanID      string          `json:"parentSpanId"`
	Name              string          `json:"name"`
	StartTimeUnixNano int64           `json:"startTimeUnixNano,string"`
	EndTimeUnixNano   int64           `json:"endTimeUnixNano,string"`
	Status            otlpStatus      `json:"status"`
	Attributes        []otlpAttribute `json:"attributes"`
}
