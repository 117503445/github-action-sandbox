package githubactions

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type artifactsResponse struct {
	Artifacts []Artifact `json:"artifacts"`
}

// Artifact is the minimal workflow artifact payload used by the SDK.
type Artifact struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Expired bool   `json:"expired"`
}

// ListWorkflowArtifacts lists artifacts for one run.
func (c *Client) ListWorkflowArtifacts(ctx context.Context, runID int64) ([]Artifact, error) {
	var resp artifactsResponse
	if err := c.doJSON(
		ctx,
		http.MethodGet,
		c.repoPath("/actions/runs/%d/artifacts", runID),
		nil,
		http.StatusOK,
		&resp,
	); err != nil {
		return nil, err
	}
	return resp.Artifacts, nil
}

// DownloadArtifactZIP downloads the ZIP payload for one artifact.
func (c *Client) DownloadArtifactZIP(ctx context.Context, artifactID int64) ([]byte, error) {
	req, err := c.newRequest(ctx, http.MethodGet, c.repoPath("/actions/artifacts/%d/zip", artifactID), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("github api %s: status %d: %s", req.URL.Path, resp.StatusCode, strings.TrimSpace(string(payload)))
	}

	return io.ReadAll(resp.Body)
}
