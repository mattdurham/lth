package memory

type SearchRequest struct {
	Query      string
	Layers     []int
	TopK       int
	Seeds      []string
	Alpha      float32
	Beta       float32
	Gamma      float32
	MinValence *float32
	MaxValence *float32
	Expand     bool
}
