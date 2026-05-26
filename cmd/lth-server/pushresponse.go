package main

type pushResponse struct {
	Accepted int `json:"accepted"`
	Skipped  int `json:"skipped"`
}
