package githubactions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const defaultAPIBaseURL = "https://api.github.com"

// Client wraps the subset of the GitHub Actions REST API used by the sandbox SDK.
type Client struct {
	baseURL    string
	repository string
	token      string
	httpClient *http.Client
}

// NewClient creates a GitHub Actions API client for one repository.
func NewClient(repository string, token string) *Client {
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("GITHUB_API_URL")), "/")
	if baseURL == "" {
		baseURL = defaultAPIBaseURL
	}

	return &Client{
		baseURL:    baseURL,
		repository: repository,
		token:      token,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// DispatchWorkflow triggers one workflow_dispatch run.
func (c *Client) DispatchWorkflow(
	ctx context.Context,
	workflow string,
	ref string,
	requestID string,
	uptermServer string,
	startupTimeout time.Duration,
) error {
	seconds := int(math.Ceil(startupTimeout.Seconds()))
	body := map[string]any{
		"ref": ref,
		"inputs": map[string]string{
			"request_id":              requestID,
			"upterm_server":           uptermServer,
			"startup_timeout_seconds": fmt.Sprintf("%d", seconds),
		},
	}

	return c.doJSON(
		ctx,
		http.MethodPost,
		c.workflowPath("/actions/workflows/%s/dispatches", workflow),
		body,
		http.StatusNoContent,
		nil,
	)
}

func (c *Client) doJSON(
	ctx context.Context,
	method string,
	apiPath string,
	body any,
	expectedStatus int,
	out any,
) error {
	req, err := c.newRequest(ctx, method, apiPath, body)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != expectedStatus {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("github api %s: status %d: %s", req.URL.Path, resp.StatusCode, strings.TrimSpace(string(payload)))
	}

	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}

	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) newRequest(ctx context.Context, method string, apiPath string, body any) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+apiPath, reader)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "github-action-sandbox-sdk")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	return req, nil
}

func (c *Client) workflowPath(format string, workflow string, args ...any) string {
	allArgs := make([]any, 0, len(args)+1)
	allArgs = append(allArgs, url.PathEscape(workflow))
	allArgs = append(allArgs, args...)
	return c.repoPath(format, allArgs...)
}

func (c *Client) repoPath(format string, args ...any) string {
	owner, repo := SplitRepository(c.repository)
	prefix := fmt.Sprintf("/repos/%s/%s", url.PathEscape(owner), url.PathEscape(repo))
	return prefix + fmt.Sprintf(format, args...)
}

// SplitRepository splits owner/repo into its components.
func SplitRepository(repository string) (string, string) {
	parts := strings.SplitN(repository, "/", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

// IsValidRepository reports whether repository matches owner/repo.
func IsValidRepository(repository string) bool {
	owner, repo := SplitRepository(repository)
	return owner != "" && repo != "" && !strings.Contains(repo, "/")
}
