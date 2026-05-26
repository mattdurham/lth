package traces

type otlpResource struct {
	Attributes []otlpAttribute `json:"attributes"`
}
