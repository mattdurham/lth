// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package traces

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
)

// Span is a parsed OpenTelemetry span ready for storage.
type Span struct {
	TraceID      string
	SpanID       string
	ParentSpanID string
	ServiceName  string
	Name         string
	StartNano    int64
	EndNano      int64
	StatusCode   int
	Attrs        map[string]string
}

// OTLP JSON types (unexported) — exactly what the OTLP HTTP/JSON spec sends.
type otlpRequest struct {
	ResourceSpans []otlpResourceSpan `json:"resourceSpans"`
}
type otlpResourceSpan struct {
	Resource   otlpResource    `json:"resource"`
	ScopeSpans []otlpScopeSpan `json:"scopeSpans"`
}
type otlpResource struct {
	Attributes []otlpAttribute `json:"attributes"`
}
type otlpScopeSpan struct {
	Spans []otlpSpan `json:"spans"`
}
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
type otlpStatus struct {
	Code int `json:"code"`
}
type otlpAttribute struct {
	Key   string       `json:"key"`
	Value otlpAnyValue `json:"value"`
}
type otlpAnyValue struct {
	StringValue *string `json:"stringValue,omitempty"`
	IntValue    *int64  `json:"intValue,string,omitempty"`
	BoolValue   *bool   `json:"boolValue,omitempty"`
}

func parseOTLP(data []byte) ([]Span, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty OTLP payload")
	}
	var req otlpRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("unmarshal OTLP: %w", err)
	}
	var spans []Span
	for _, rs := range req.ResourceSpans {
		svcName := ""
		for _, a := range rs.Resource.Attributes {
			if a.Key == "service.name" {
				svcName = attrStringValue(a.Value)
				break
			}
		}
		for _, ss := range rs.ScopeSpans {
			for _, s := range ss.Spans {
				attrs := make(map[string]string, len(s.Attributes))
				for _, a := range s.Attributes {
					attrs[a.Key] = attrStringValue(a.Value)
				}
				spans = append(spans, Span{
					TraceID:      base64ToHex(s.TraceID),
					SpanID:       base64ToHex(s.SpanID),
					ParentSpanID: base64ToHex(s.ParentSpanID),
					ServiceName:  svcName,
					Name:         s.Name,
					StartNano:    s.StartTimeUnixNano,
					EndNano:      s.EndTimeUnixNano,
					StatusCode:   s.Status.Code,
					Attrs:        attrs,
				})
			}
		}
	}
	return spans, nil
}

func spanContent(s Span) string {
	durationMs := (s.EndNano - s.StartNano) / 1e6
	statusStr := "UNSET"
	switch s.StatusCode {
	case 1:
		statusStr = "OK"
	case 2:
		statusStr = "ERROR"
	}
	return fmt.Sprintf("[%s] span: %s duration=%dms status=%s", s.ServiceName, s.Name, durationMs, statusStr)
}

func base64ToHex(s string) string {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return s
	}
	return hex.EncodeToString(b)
}

func attrStringValue(av otlpAnyValue) string {
	if av.StringValue != nil {
		return *av.StringValue
	}
	if av.IntValue != nil {
		return strconv.FormatInt(*av.IntValue, 10)
	}
	if av.BoolValue != nil {
		if *av.BoolValue {
			return "true"
		}
		return "false"
	}
	return ""
}
