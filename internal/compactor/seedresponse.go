// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package compactor

type seedResponse struct {
	Rules  []string    `json:"rules"`
	Skills []seedSkill `json:"skills"`
}
