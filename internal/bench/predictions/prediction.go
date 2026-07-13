// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package predictions

type Prediction struct {
	InstanceID string `json:"instance_id"`
	ModelPatch string `json:"model_patch"`
	ModelName  string `json:"model_name_or_path"`
}
