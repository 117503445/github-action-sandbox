package sandbox

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDefaultCreateSandboxOptions(t *testing.T) {
	opts := DefaultCreateSandboxOptions()

	if opts.GitHubWorkflow != "sandbox.yml" {
		t.Fatalf("unexpected workflow default: %q", opts.GitHubWorkflow)
	}
	if opts.GitHubRef != "main" {
		t.Fatalf("unexpected ref default: %q", opts.GitHubRef)
	}
	if opts.UptermServer != "ssh://uptermd.upterm.dev:22" {
		t.Fatalf("unexpected upterm server default: %q", opts.UptermServer)
	}
	if opts.StartupTimeout != 2*time.Minute {
		t.Fatalf("unexpected timeout default: %s", opts.StartupTimeout)
	}
}

func TestCreateSandboxInvalidOptions(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")

	_, err := CreateSandbox(context.Background(), CreateSandboxOptions{})
	if !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("expected ErrInvalidOptions, got %v", err)
	}
}

func TestCreateSandboxDispatchesWorkflowAndReturnsMetadata(t *testing.T) {
	setTestPollInterval(t, 10*time.Millisecond)

	type dispatchPayload struct {
		Ref    string            `json:"ref"`
		Inputs map[string]string `json:"inputs"`
	}

	var state struct {
		sync.Mutex
		dispatchBody dispatchPayload
		authHeader   string
		runPolls     int
		artifactPoll int
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state.Lock()
		defer state.Unlock()

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/widgets/actions/workflows/custom.yml/dispatches":
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&state.dispatchBody); err != nil {
				t.Fatalf("decode dispatch body: %v", err)
			}
			state.authHeader = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widgets/actions/workflows/custom.yml/runs":
			state.runPolls++
			status := "queued"
			if state.runPolls >= 2 {
				status = "in_progress"
			}
			writeJSON(t, w, map[string]any{
				"workflow_runs": []map[string]any{{
					"id":            int64(123),
					"display_title": "sandbox-" + state.dispatchBody.Inputs["request_id"],
					"status":        status,
					"conclusion":    "",
					"html_url":      "https://example.test/runs/123",
					"created_at":    "2026-04-17T00:00:00Z",
				}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widgets/actions/runs/123/artifacts":
			state.artifactPoll++
			artifacts := []map[string]any{}
			if state.artifactPoll >= 2 {
				artifacts = append(artifacts, map[string]any{
					"id":   int64(55),
					"name": "sandbox-" + state.dispatchBody.Inputs["request_id"],
				})
			}
			writeJSON(t, w, map[string]any{"artifacts": artifacts})
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widgets/actions/runs/123":
			writeJSON(t, w, map[string]any{
				"id":         int64(123),
				"status":     "in_progress",
				"conclusion": "",
				"html_url":   "https://example.test/runs/123",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widgets/actions/artifacts/55/zip":
			w.Header().Set("Content-Type", "application/zip")
			if _, err := w.Write(buildMetadataArchive(t, sandboxMetadata{
				RequestID:  state.dispatchBody.Inputs["request_id"],
				Status:     "running",
				SSHHost:    "uptermd.upterm.dev",
				SSHPort:    22,
				SSHUser:    "session-token",
				SSHCommand: "ssh -p 22 session-token@uptermd.upterm.dev",
			})); err != nil {
				t.Fatalf("write archive: %v", err)
			}
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	t.Setenv("GITHUB_API_URL", server.URL)
	t.Setenv("GITHUB_TOKEN", "env-token")

	sbx, err := CreateSandbox(context.Background(), CreateSandboxOptions{
		Name:             "Demo Runner",
		GitHubRepository: "acme/widgets",
		GitHubWorkflow:   "custom.yml",
		GitHubRef:        "feature/demo",
		GitHubToken:      "opts-token",
		UptermServer:     "ssh://custom.upterm.dev:2222",
		StartupTimeout:   2 * time.Second,
	})
	if err != nil {
		t.Fatalf("CreateSandbox returned error: %v", err)
	}

	if sbx.ID == "" || !strings.HasPrefix(sbx.ID, "demo-runner-") {
		t.Fatalf("unexpected sandbox id: %q", sbx.ID)
	}
	if sbx.Status != "running" {
		t.Fatalf("unexpected sandbox status: %q", sbx.Status)
	}
	if sbx.Repository != "acme/widgets" || sbx.Workflow != "custom.yml" || sbx.Ref != "feature/demo" {
		t.Fatalf("unexpected repository info: %+v", sbx)
	}
	if sbx.RunID != 123 || sbx.RunURL != "https://example.test/runs/123" {
		t.Fatalf("unexpected run info: %+v", sbx)
	}
	if sbx.SSHHost != "uptermd.upterm.dev" || sbx.SSHPort != 22 || sbx.SSHUser != "session-token" {
		t.Fatalf("unexpected ssh info: %+v", sbx)
	}
	if sbx.SSHCommand != "ssh -p 22 session-token@uptermd.upterm.dev" {
		t.Fatalf("unexpected ssh command: %q", sbx.SSHCommand)
	}

	state.Lock()
	defer state.Unlock()
	if state.authHeader != "Bearer opts-token" {
		t.Fatalf("expected explicit token to win, got auth header %q", state.authHeader)
	}
	if state.dispatchBody.Ref != "feature/demo" {
		t.Fatalf("unexpected dispatch ref: %q", state.dispatchBody.Ref)
	}
	if state.dispatchBody.Inputs["request_id"] != sbx.ID {
		t.Fatalf("dispatch request_id mismatch: %q vs %q", state.dispatchBody.Inputs["request_id"], sbx.ID)
	}
	if state.dispatchBody.Inputs["upterm_server"] != "ssh://custom.upterm.dev:2222" {
		t.Fatalf("unexpected upterm server input: %q", state.dispatchBody.Inputs["upterm_server"])
	}
	if state.dispatchBody.Inputs["startup_timeout_seconds"] != "2" {
		t.Fatalf("unexpected startup timeout input: %q", state.dispatchBody.Inputs["startup_timeout_seconds"])
	}
}

func TestCreateSandboxReturnsSandboxFailedWhenWorkflowCompletesEarly(t *testing.T) {
	setTestPollInterval(t, 10*time.Millisecond)

	type dispatchPayload struct {
		Inputs map[string]string `json:"inputs"`
	}

	var state struct {
		sync.Mutex
		dispatchBody dispatchPayload
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state.Lock()
		defer state.Unlock()

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/widgets/actions/workflows/sandbox.yml/dispatches":
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&state.dispatchBody); err != nil {
				t.Fatalf("decode dispatch body: %v", err)
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widgets/actions/workflows/sandbox.yml/runs":
			writeJSON(t, w, map[string]any{
				"workflow_runs": []map[string]any{{
					"id":            int64(123),
					"display_title": "sandbox-" + state.dispatchBody.Inputs["request_id"],
					"status":        "in_progress",
					"conclusion":    "",
					"created_at":    "2026-04-17T00:00:00Z",
				}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widgets/actions/runs/123/artifacts":
			writeJSON(t, w, map[string]any{"artifacts": []map[string]any{}})
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widgets/actions/runs/123":
			writeJSON(t, w, map[string]any{
				"id":         int64(123),
				"status":     "completed",
				"conclusion": "failure",
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	t.Setenv("GITHUB_API_URL", server.URL)
	t.Setenv("GITHUB_TOKEN", "token")

	_, err := CreateSandbox(context.Background(), CreateSandboxOptions{
		GitHubRepository: "acme/widgets",
		StartupTimeout:   time.Second,
	})
	if !errors.Is(err, ErrSandboxFailed) {
		t.Fatalf("expected ErrSandboxFailed, got %v", err)
	}
}

func TestCreateSandboxReturnsMetadataTimeout(t *testing.T) {
	setTestPollInterval(t, 10*time.Millisecond)

	type dispatchPayload struct {
		Inputs map[string]string `json:"inputs"`
	}

	var state struct {
		sync.Mutex
		dispatchBody dispatchPayload
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state.Lock()
		defer state.Unlock()

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/widgets/actions/workflows/sandbox.yml/dispatches":
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&state.dispatchBody); err != nil {
				t.Fatalf("decode dispatch body: %v", err)
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widgets/actions/workflows/sandbox.yml/runs":
			writeJSON(t, w, map[string]any{
				"workflow_runs": []map[string]any{{
					"id":            int64(123),
					"display_title": "sandbox-" + state.dispatchBody.Inputs["request_id"],
					"status":        "in_progress",
					"conclusion":    "",
					"created_at":    "2026-04-17T00:00:00Z",
				}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widgets/actions/runs/123/artifacts":
			writeJSON(t, w, map[string]any{"artifacts": []map[string]any{}})
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widgets/actions/runs/123":
			writeJSON(t, w, map[string]any{
				"id":         int64(123),
				"status":     "in_progress",
				"conclusion": "",
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	t.Setenv("GITHUB_API_URL", server.URL)
	t.Setenv("GITHUB_TOKEN", "token")

	_, err := CreateSandbox(context.Background(), CreateSandboxOptions{
		GitHubRepository: "acme/widgets",
		StartupTimeout:   50 * time.Millisecond,
	})
	if !errors.Is(err, ErrMetadataTimeout) {
		t.Fatalf("expected ErrMetadataTimeout, got %v", err)
	}
}

func TestCloseCancelsRunAndWaitsForCompletion(t *testing.T) {
	setTestPollInterval(t, 10*time.Millisecond)

	var state struct {
		sync.Mutex
		cancelCalls int
		runCalls    int
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state.Lock()
		defer state.Unlock()

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/widgets/actions/runs/123/cancel":
			state.cancelCalls++
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widgets/actions/runs/123":
			state.runCalls++
			status := "in_progress"
			conclusion := ""
			if state.runCalls >= 2 {
				status = "completed"
				conclusion = "cancelled"
			}
			writeJSON(t, w, map[string]any{
				"id":         int64(123),
				"status":     status,
				"conclusion": conclusion,
				"html_url":   "https://example.test/runs/123",
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	t.Setenv("GITHUB_API_URL", server.URL)

	sbx := &Sandbox{
		Repository: "acme/widgets",
		RunID:      123,
		client:     newGitHubActionsClient("acme/widgets", "token"),
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := sbx.Close(ctx); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if sbx.Status != "cancelled" {
		t.Fatalf("unexpected sandbox status after close: %q", sbx.Status)
	}
	if sbx.RunURL != "https://example.test/runs/123" {
		t.Fatalf("unexpected run url after close: %q", sbx.RunURL)
	}

	state.Lock()
	defer state.Unlock()
	if state.cancelCalls != 1 {
		t.Fatalf("expected one cancel call, got %d", state.cancelCalls)
	}
}

func setTestPollInterval(t *testing.T, d time.Duration) {
	t.Helper()
	old := pollInterval
	pollInterval = d
	t.Cleanup(func() {
		pollInterval = old
	})
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("encode json: %v", err)
	}
}

func buildMetadataArchive(t *testing.T, metadata sandboxMetadata) []byte {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	file, err := zw.Create("metadata.json")
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if err := json.NewEncoder(file).Encode(metadata); err != nil {
		t.Fatalf("encode metadata: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}

	return buf.Bytes()
}
