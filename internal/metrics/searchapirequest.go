package metrics

type searchAPIRequest struct {
	Query      string   `json:"query"`
	Layers     []int    `json:"layers"`
	TopK       int      `json:"topK"`
	Alpha      float32  `json:"alpha"`
	Beta       float32  `json:"beta"`
	Gamma      float32  `json:"gamma"`
	MinValence *float32 `json:"minValence,omitempty"`
	MaxValence *float32 `json:"maxValence,omitempty"`
}
