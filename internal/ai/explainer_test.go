package ai

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// TestNewExplainer_Defaults verifies that NewExplainer falls back to
// sensible defaults when no env vars are set.
func TestNewExplainer_Defaults(t *testing.T) {
	// Clear env vars to test defaults
	os.Unsetenv("LOSU_OLLAMA_HOST")
	os.Unsetenv("LOSU_AI_MODEL")

	e := NewExplainer()

	if e.Endpoint == "" {
		t.Error("Endpoint should not be empty")
	}
	if e.Model == "" {
		t.Error("Model should not be empty")
	}
	// Default model should be llama3:latest
	if e.Model != "llama3:latest" {
		t.Errorf("Expected default model 'llama3:latest', got %q", e.Model)
	}
}

// TestNewExplainer_EnvOverride verifies that env vars are respected.
func TestNewExplainer_EnvOverride(t *testing.T) {
	os.Setenv("LOSU_AI_MODEL", "phi3:latest")
	defer os.Unsetenv("LOSU_AI_MODEL")

	// Set a non-docker host so it doesn't get overridden to localhost
	os.Setenv("LOSU_OLLAMA_HOST", "http://my-ollama:11434")
	defer os.Unsetenv("LOSU_OLLAMA_HOST")

	e := NewExplainer()

	if e.Model != "phi3:latest" {
		t.Errorf("Expected model 'phi3:latest', got %q", e.Model)
	}
	if e.Endpoint != "http://my-ollama:11434/api/generate" {
		t.Errorf("Expected custom endpoint, got %q", e.Endpoint)
	}
}

// TestNewExplainer_DockerHostOverride verifies that the docker host
// "http://ollama:11434" is replaced with localhost when running locally.
func TestNewExplainer_DockerHostOverride(t *testing.T) {
	os.Setenv("LOSU_OLLAMA_HOST", "http://ollama:11434")
	defer os.Unsetenv("LOSU_OLLAMA_HOST")

	e := NewExplainer()

	if e.Endpoint != "http://localhost:11434/api/generate" {
		t.Errorf("Expected docker host to be overridden to localhost, got %q", e.Endpoint)
	}
}

// TestAnalyzeSystem_HTTPError verifies that AnalyzeSystem returns an error
// when the Ollama endpoint is unreachable — app should not crash.
func TestAnalyzeSystem_HTTPError(t *testing.T) {
	e := &Explainer{
		Endpoint: "http://localhost:19999/api/generate", // nothing listening here
		Model:    "llama3:latest",
	}

	_, err := e.AnalyzeSystem("err patterns", "warn patterns", 1.0, 5.0, 0.5, 2.0)
	if err == nil {
		t.Error("Expected error for unreachable endpoint, got nil")
	}
}

// TestAnalyzeSystem_MockServer verifies the full request/response cycle
// using a local test HTTP server — no real Ollama needed.
func TestAnalyzeSystem_MockServer(t *testing.T) {
	// Mock Ollama server that returns a valid response
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request body contains expected fields
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("failed to decode request body: %v", err)
			http.Error(w, "bad request", 400)
			return
		}
		if payload["model"] == nil {
			t.Error("request missing model field")
		}
		if payload["prompt"] == nil {
			t.Error("request missing prompt field")
		}

		// Return a mock Ollama response
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"response": "**Primary Incident**: DB timeout spike detected.",
		})
	}))
	defer srv.Close()

	e := &Explainer{
		Endpoint: srv.URL,
		Model:    "llama3:latest",
	}

	result, err := e.AnalyzeSystem("DB timeout x10", "High memory x5", 2.5, 8.0, 4.0, 12.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Error("Expected non-empty response from mock server")
	}
	if result != "**Primary Incident**: DB timeout spike detected." {
		t.Errorf("unexpected response: %q", result)
	}
}

// TestAnalyzeHeartbeat_MockServer verifies the heartbeat analysis path
// using a mock server.
func TestAnalyzeHeartbeat_MockServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"response": "**Status**: Stable. No action required.",
		})
	}))
	defer srv.Close()

	e := &Explainer{
		Endpoint: srv.URL,
		Model:    "llama3:latest",
	}

	result, err := e.AnalyzeHeartbeat(
		map[string]int{"ERROR": 3, "WARN": 12, "INFO": 500},
		"[ERROR] DB timeout (3 times)",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Error("Expected non-empty response")
	}
}

// TestAnalyzeHeartbeat_HTTPError verifies graceful error handling.
func TestAnalyzeHeartbeat_HTTPError(t *testing.T) {
	e := &Explainer{
		Endpoint: "http://localhost:19999/api/generate",
		Model:    "llama3:latest",
	}

	_, err := e.AnalyzeHeartbeat(map[string]int{"ERROR": 1}, "err")
	if err == nil {
		t.Error("Expected error for unreachable endpoint, got nil")
	}
}
