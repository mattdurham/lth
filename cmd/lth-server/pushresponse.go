// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

type pushResponse struct {
	Accepted int `json:"accepted"`
	Skipped  int `json:"skipped"`
}
