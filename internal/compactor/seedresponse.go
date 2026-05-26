package compactor

type seedResponse struct {
	Rules  []string    `json:"rules"`
	Skills []seedSkill `json:"skills"`
}
