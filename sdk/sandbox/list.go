package sandbox

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/117503445/github-action-sandbox/internal/githubactions"

	"github.com/rs/zerolog"
)

type resolvedListSandboxesOptions struct {
	GitHubRepository string
	GitHubWorkflow   string
	GitHubToken      string

	Limit int
}

// ListSandboxes 列出一个 workflow 下最近的 sandbox runs。
func ListSandboxes(ctx context.Context, opts ListSandboxesOptions) ([]*Sandbox, error) {
	resolved, err := resolveListSandboxesOptions(opts)
	if err != nil {
		return nil, err
	}

	logger := zerolog.Ctx(ctx)
	client := newGitHubActionsClient(resolved.GitHubRepository, resolved.GitHubToken)

	runs, err := client.ListWorkflowRuns(ctx, resolved.GitHubWorkflow, resolved.Limit)
	if err != nil {
		return nil, err
	}

	items := make([]*Sandbox, 0, len(runs))
	for _, run := range runs {
		item := sandboxFromWorkflowRun(resolved.GitHubRepository, resolved.GitHubWorkflow, client, run)

		metadata, ok, err := loadMetadataForRun(ctx, client, run, item.ID)
		if err != nil {
			logger.Warn().
				Int64("run_id", run.ID).
				Err(err).
				Msg("failed to enrich sandbox metadata")
		} else if ok {
			applySandboxMetadata(item, metadata)
		}

		items = append(items, item)
	}

	return items, nil
}

func resolveListSandboxesOptions(opts ListSandboxesOptions) (resolvedListSandboxesOptions, error) {
	defaults := DefaultListSandboxesOptions()

	resolved := resolvedListSandboxesOptions{
		GitHubRepository: strings.TrimSpace(opts.GitHubRepository),
		GitHubWorkflow:   strings.TrimSpace(opts.GitHubWorkflow),
		GitHubToken:      strings.TrimSpace(opts.GitHubToken),
		Limit:            opts.Limit,
	}

	if resolved.GitHubWorkflow == "" {
		resolved.GitHubWorkflow = defaults.GitHubWorkflow
	}
	if resolved.Limit == 0 {
		resolved.Limit = defaults.Limit
	}
	if resolved.GitHubToken == "" {
		resolved.GitHubToken = strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
	}

	if !githubactions.IsValidRepository(resolved.GitHubRepository) {
		return resolvedListSandboxesOptions{}, fmt.Errorf("%w: invalid GitHub repository", ErrInvalidOptions)
	}
	if resolved.GitHubWorkflow == "" {
		return resolvedListSandboxesOptions{}, fmt.Errorf("%w: missing GitHub workflow", ErrInvalidOptions)
	}
	if resolved.GitHubToken == "" {
		return resolvedListSandboxesOptions{}, fmt.Errorf("%w: missing GitHub token", ErrInvalidOptions)
	}
	if resolved.Limit <= 0 || resolved.Limit > 100 {
		return resolvedListSandboxesOptions{}, fmt.Errorf("%w: limit must be between 1 and 100", ErrInvalidOptions)
	}

	return resolved, nil
}

func sandboxFromWorkflowRun(
	repository string,
	workflow string,
	client *githubactions.Client,
	run githubactions.WorkflowRun,
) *Sandbox {
	return &Sandbox{
		ID:         extractRequestIDFromRun(run),
		Status:     run.EffectiveStatus(),
		Repository: repository,
		Workflow:   workflow,
		Ref:        run.HeadBranch,
		RunID:      run.ID,
		RunURL:     run.HTMLURL,
		CreatedAt:  run.CreatedAt.Time,
		client:     client,
	}
}

func loadMetadataForRun(
	ctx context.Context,
	client *githubactions.Client,
	run githubactions.WorkflowRun,
	requestID string,
) (sandboxMetadata, bool, error) {
	artifacts, err := client.ListWorkflowArtifacts(ctx, run.ID)
	if err != nil {
		return sandboxMetadata{}, false, err
	}

	artifact, ok := findSandboxArtifact(artifacts, requestID)
	if !ok {
		return sandboxMetadata{}, false, nil
	}

	payload, err := client.DownloadArtifactZIP(ctx, artifact.ID)
	if err != nil {
		return sandboxMetadata{}, false, err
	}

	metadata, err := parseMetadataArchive(payload)
	if err != nil {
		return sandboxMetadata{}, false, err
	}

	return metadata, true, nil
}

func findSandboxArtifact(artifacts []githubactions.Artifact, requestID string) (githubactions.Artifact, bool) {
	if requestID != "" {
		expectedName := "sandbox-" + requestID
		for _, artifact := range artifacts {
			if artifact.Expired || artifact.Name != expectedName {
				continue
			}
			return artifact, true
		}
	}

	for _, artifact := range artifacts {
		if artifact.Expired || !strings.HasPrefix(artifact.Name, "sandbox-") {
			continue
		}
		return artifact, true
	}

	return githubactions.Artifact{}, false
}

func applySandboxMetadata(item *Sandbox, metadata sandboxMetadata) {
	if metadata.RequestID != "" {
		item.ID = metadata.RequestID
	}
	if shouldOverrideSandboxStatus(item.Status) && metadata.Status != "" {
		item.Status = metadata.Status
	}
	if metadata.SSHHost != "" {
		item.SSHHost = metadata.SSHHost
	}
	if metadata.SSHPort > 0 {
		item.SSHPort = metadata.SSHPort
	}
	if metadata.SSHUser != "" {
		item.SSHUser = metadata.SSHUser
	}
	if metadata.SSHCommand != "" {
		item.SSHCommand = metadata.SSHCommand
	}
}

func shouldOverrideSandboxStatus(status string) bool {
	switch status {
	case "", "queued", "in_progress", "waiting", "pending", "requested":
		return true
	default:
		return false
	}
}

func extractRequestIDFromRun(run githubactions.WorkflowRun) string {
	for _, field := range []string{run.DisplayTitle, run.RunName, run.Name} {
		if requestID := extractRequestIDFromText(field); requestID != "" {
			return requestID
		}
	}

	return ""
}

func extractRequestIDFromText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	lower := strings.ToLower(value)
	index := strings.Index(lower, "sandbox-")
	if index < 0 {
		return ""
	}

	suffix := strings.TrimSpace(value[index+len("sandbox-"):])
	if suffix == "" {
		return ""
	}

	fields := strings.Fields(suffix)
	if len(fields) == 0 {
		return ""
	}

	return strings.Trim(fields[0], "[](){}")
}
