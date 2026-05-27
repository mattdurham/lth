package predictions

type Prediction struct {
	InstanceID string `json:"instance_id"`
	ModelPatch string `json:"model_patch"`
	ModelName  string `json:"model_name_or_path"`
}
