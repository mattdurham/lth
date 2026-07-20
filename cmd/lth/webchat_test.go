// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mattdurham/lth/pkg/lth"
)

func TestHandleWebChatPage_ServesHTML(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/chat", nil)
	w := httptest.NewRecorder()

	handleWebChatPage(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if ct == "" || ct[:9] != "text/html" {
		t.Fatalf("expected text/html content-type, got %q", ct)
	}
	if w.Body.Len() == 0 {
		t.Fatal("expected non-empty body")
	}
}

func TestHandleWebChatAPI_HappyPath(t *testing.T) {
	orig := doChatFn
	doChatFn = func(_ context.Context, _ *lth.Client, question string, history []chatTurn, _ map[string]string) (string, error) {
		return "hello back", nil
	}
	t.Cleanup(func() { doChatFn = orig })

	body := `{"message":"hi","history":[],"store":false}`
	req := httptest.NewRequest(http.MethodPost, "/chat", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleWebChatAPI(w, req, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp webChatResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Reply != "hello back" {
		t.Errorf("expected reply %q, got %q", "hello back", resp.Reply)
	}
	if len(resp.History) != 1 {
		t.Fatalf("expected 1 history item, got %d", len(resp.History))
	}
	if resp.History[0].User != "hi" || resp.History[0].Assistant != "hello back" {
		t.Errorf("unexpected history item: %+v", resp.History[0])
	}
}

func TestHandleWebChatAPI_WithExistingHistory(t *testing.T) {
	var capturedHistory []chatTurn
	orig := doChatFn
	doChatFn = func(_ context.Context, _ *lth.Client, question string, history []chatTurn, _ map[string]string) (string, error) {
		capturedHistory = history
		return "answer2", nil
	}
	t.Cleanup(func() { doChatFn = orig })

	body := `{"message":"q2","history":[{"user":"q1","assistant":"a1"}],"store":false}`
	req := httptest.NewRequest(http.MethodPost, "/chat", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleWebChatAPI(w, req, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(capturedHistory) != 1 || capturedHistory[0].user != "q1" || capturedHistory[0].assistant != "a1" {
		t.Errorf("expected prior history to be passed through, got %+v", capturedHistory)
	}

	var resp webChatResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.History) != 2 {
		t.Fatalf("expected 2 history items, got %d", len(resp.History))
	}
	if resp.History[1].User != "q2" || resp.History[1].Assistant != "answer2" {
		t.Errorf("unexpected second history item: %+v", resp.History[1])
	}
}

func TestHandleWebChatAPI_EmptyMessage(t *testing.T) {
	called := false
	orig := doChatFn
	doChatFn = func(_ context.Context, _ *lth.Client, _ string, _ []chatTurn, _ map[string]string) (string, error) {
		called = true
		return "", nil
	}
	t.Cleanup(func() { doChatFn = orig })

	body := `{"message":"","history":[],"store":false}`
	req := httptest.NewRequest(http.MethodPost, "/chat", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleWebChatAPI(w, req, nil)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if called {
		t.Error("doChatFn should not be called for empty message")
	}
}

func TestHandleWebChatAPI_DoChatError(t *testing.T) {
	orig := doChatFn
	doChatFn = func(_ context.Context, _ *lth.Client, _ string, _ []chatTurn, _ map[string]string) (string, error) {
		return "", errors.New("llm down")
	}
	t.Cleanup(func() { doChatFn = orig })

	body := `{"message":"hello","history":[],"store":false}`
	req := httptest.NewRequest(http.MethodPost, "/chat", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleWebChatAPI(w, req, nil)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestHandleWebChatAPI_ProjectSetsFilterAttrs(t *testing.T) {
	var capturedAttrs map[string]string
	orig := doChatFn
	doChatFn = func(_ context.Context, _ *lth.Client, _ string, _ []chatTurn, filterAttrs map[string]string) (string, error) {
		capturedAttrs = filterAttrs
		return "answer", nil
	}
	t.Cleanup(func() { doChatFn = orig })

	body := `{"message":"hi","history":[],"store":false,"project":"grafana/tempo"}`
	req := httptest.NewRequest(http.MethodPost, "/chat", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleWebChatAPI(w, req, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if capturedAttrs["project"] != "grafana/tempo" {
		t.Errorf("filterAttrs[project] = %q, want %q", capturedAttrs["project"], "grafana/tempo")
	}
}

func TestHandleWebChatAPI_NoProjectLeavesFilterAttrsNil(t *testing.T) {
	var capturedAttrs map[string]string
	called := false
	orig := doChatFn
	doChatFn = func(_ context.Context, _ *lth.Client, _ string, _ []chatTurn, filterAttrs map[string]string) (string, error) {
		called = true
		capturedAttrs = filterAttrs
		return "answer", nil
	}
	t.Cleanup(func() { doChatFn = orig })

	body := `{"message":"hi","history":[],"store":false}`
	req := httptest.NewRequest(http.MethodPost, "/chat", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleWebChatAPI(w, req, nil)

	if !called {
		t.Fatal("doChatFn was not called")
	}
	if capturedAttrs != nil {
		t.Errorf("filterAttrs = %v, want nil when no project given", capturedAttrs)
	}
}

func TestHandleWebChatAPI_BadJSON(t *testing.T) {
	orig := doChatFn
	doChatFn = func(_ context.Context, _ *lth.Client, _ string, _ []chatTurn, _ map[string]string) (string, error) {
		return "", nil
	}
	t.Cleanup(func() { doChatFn = orig })

	req := httptest.NewRequest(http.MethodPost, "/chat", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleWebChatAPI(w, req, nil)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
