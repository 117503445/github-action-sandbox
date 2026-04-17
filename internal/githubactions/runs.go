package githubactions

import "context"

type workflowRunsResponse struct {
	WorkflowRuns []WorkflowRun `json:"workflow_runs"`
}

// WorkflowRun is the minimal workflow run payload used by the SDK.
type WorkflowRun struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	DisplayTitle string    `json:"display_title"`
	RunName      string    `json:"run_name"`
	Status       string    `json:"status"`
	Conclusion   string    `json:"conclusion"`
	HTMLURL      string    `json:"html_url"`
	CreatedAt    TimeValue `json:"created_at"`
}

// EffectiveStatus collapses completed runs to their conclusion when available.
func (r WorkflowRun) EffectiveStatus() string {
	if r.Conclusion != "" {
		return r.Conclusion
	}
	return r.Status
}

// ListWorkflowRuns returns recent workflow_dispatch runs for one workflow file.
func (c *Client) ListWorkflowRuns(ctx context.Context, workflow string) ([]WorkflowRun, error) {
	var resp workflowRunsResponse
	if err := c.doJSON(
		ctx,
		"GET",
		c.workflowPath("/actions/workflows/%s/runs?event=workflow_dispatch&per_page=20", workflow),
		nil,
		200,
		&resp,
	); err != nil {
		return nil, err
	}
	return resp.WorkflowRuns, nil
}

// GetWorkflowRun loads a single workflow run.
func (c *Client) GetWorkflowRun(ctx context.Context, runID int64) (WorkflowRun, error) {
	var run WorkflowRun
	err := c.doJSON(ctx, "GET", c.repoPath("/actions/runs/%d", runID), nil, 200, &run)
	return run, err
}

// CancelWorkflowRun sends a cancel request for one run.
func (c *Client) CancelWorkflowRun(ctx context.Context, runID int64) error {
	return c.doJSON(ctx, "POST", c.repoPath("/actions/runs/%d/cancel", runID), nil, 202, nil)
}
