// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package dataset

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func makeHFResponse(problems []Problem) string {
	rows := make([]map[string]interface{}, len(problems))
	for i, p := range problems {
		rows[i] = map[string]interface{}{"row": p}
	}
	b, _ := json.Marshal(map[string]interface{}{"rows": rows})
	return string(b)
}

func TestFetchProblemsReturnsProblems(t *testing.T) {
	problems := []Problem{
		{Repo: "gin-gonic/gin", InstanceID: "gin-gonic__gin-1", FailToPass: []string{"TestFoo"}},
		{Repo: "prometheus/prometheus", InstanceID: "prometheus__prometheus-2", FailToPass: []string{"TestBar"}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(makeHFResponse(problems)))
	}))
	defer srv.Close()

	c := &HFClient{httpClient: &http.Client{}, baseURL: srv.URL}
	got, err := c.FetchProblems(context.Background(), 0, 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("got %d problems, want 2", len(got))
	}
	if got[0].InstanceID != "gin-gonic__gin-1" {
		t.Errorf("got InstanceID %q", got[0].InstanceID)
	}
}

func TestFetchProblemsLanguageFilter(t *testing.T) {
	problems := []Problem{
		{Repo: "gin-gonic/gin", InstanceID: "gin-1"},
		{Repo: "unknown/python-repo", InstanceID: "py-1"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(makeHFResponse(problems)))
	}))
	defer srv.Close()

	c := &HFClient{httpClient: &http.Client{}, baseURL: srv.URL}
	got, err := c.FetchProblems(context.Background(), 0, 10, "go")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("got %d problems after filter, want 1", len(got))
	}
	if got[0].InstanceID != "gin-1" {
		t.Errorf("unexpected InstanceID %q", got[0].InstanceID)
	}
}

func TestFetchProblemsNoLanguageFilter(t *testing.T) {
	problems := []Problem{
		{Repo: "gin-gonic/gin", InstanceID: "gin-1"},
		{Repo: "unknown/python-repo", InstanceID: "py-1"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(makeHFResponse(problems)))
	}))
	defer srv.Close()

	c := &HFClient{httpClient: &http.Client{}, baseURL: srv.URL}
	got, err := c.FetchProblems(context.Background(), 0, 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("got %d problems with no filter, want 2", len(got))
	}
}

func TestFetchProblemsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := &HFClient{httpClient: &http.Client{}, baseURL: srv.URL}
	_, err := c.FetchProblems(context.Background(), 0, 10, "")
	if err == nil {
		t.Fatal("expected error on HTTP 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status code, got: %v", err)
	}
}

func TestFetchProblemsMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{not valid json`))
	}))
	defer srv.Close()

	c := &HFClient{httpClient: &http.Client{}, baseURL: srv.URL}
	_, err := c.FetchProblems(context.Background(), 0, 10, "")
	if err == nil {
		t.Fatal("expected error on malformed JSON")
	}
}

func TestFetchProblemsURLParams(t *testing.T) {
	var capturedURL *url.URL
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL
		w.Write([]byte(makeHFResponse(nil)))
	}))
	defer srv.Close()

	c := &HFClient{httpClient: &http.Client{}, baseURL: srv.URL}
	_, err := c.FetchProblems(context.Background(), 0, 20, "")
	if err != nil {
		t.Fatal(err)
	}

	// Paginating fetcher always starts at raw offset 0 with page size 100.
	q := capturedURL.Query()
	if q.Get("offset") != "0" {
		t.Errorf("offset = %q, want \"0\"", q.Get("offset"))
	}
	if q.Get("length") != "100" {
		t.Errorf("length = %q, want \"100\"", q.Get("length"))
	}
	if q.Get("dataset") != "SWE-bench/SWE-bench_Multilingual" {
		t.Errorf("dataset = %q", q.Get("dataset"))
	}
	if q.Get("config") != "default" {
		t.Errorf("config = %q", q.Get("config"))
	}
	if q.Get("split") != "test" {
		t.Errorf("split = %q", q.Get("split"))
	}
}

func TestFetchProblemsFailToPassAsStringSlice(t *testing.T) {
	problems := []Problem{
		{Repo: "gin-gonic/gin", InstanceID: "gin-1", FailToPass: []string{"TestA", "TestB"}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(makeHFResponse(problems)))
	}))
	defer srv.Close()

	c := &HFClient{httpClient: &http.Client{}, baseURL: srv.URL}
	got, err := c.FetchProblems(context.Background(), 0, 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got[0].FailToPass) != 2 {
		t.Errorf("FailToPass len = %d, want 2", len(got[0].FailToPass))
	}
}
