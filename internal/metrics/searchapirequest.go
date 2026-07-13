// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

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
