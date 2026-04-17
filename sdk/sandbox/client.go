package sandbox

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/117503445/github-action-sandbox/internal/githubactions"

	"github.com/rs/zerolog"
)

var pollInterval = 200 * time.Millisecond

type resolvedCreateSandboxOptions struct {
	Name string

	GitHubRepository string
	GitHubWorkflow   string
	GitHubRef        string
	GitHubToken      string

	UptermServer string

	StartupTimeout time.Duration
}

type sandboxMetadata struct {
	RequestID  string `json:"request_id"`
	Status     string `json:"status"`
	SSHHost    string `json:"ssh_host"`
	SSHPort    int    `json:"ssh_port"`
	SSHUser    string `json:"ssh_user"`
	SSHCommand string `json:"ssh_command"`
}

// CreateSandbox 创建一个新的 GitHub Actions sandbox。
func CreateSandbox(ctx context.Context, opts CreateSandboxOptions) (*Sandbox, error) {
	resolved, err := resolveCreateSandboxOptions(opts)
	if err != nil {
		return nil, err
	}

	logger := zerolog.Ctx(ctx)
	client := newGitHubActionsClient(resolved.GitHubRepository, resolved.GitHubToken)
	requestID := newRequestID(resolved.Name)
	dispatchStartedAt := time.Now().UTC()
	deadline := dispatchStartedAt.Add(resolved.StartupTimeout)

	logger.Info().
		Str("request_id", requestID).
		Str("repository", resolved.GitHubRepository).
		Str("workflow", resolved.GitHubWorkflow).
		Str("ref", resolved.GitHubRef).
		Msg("dispatching sandbox workflow")

	if err := client.DispatchWorkflow(
		ctx,
		resolved.GitHubWorkflow,
		resolved.GitHubRef,
		requestID,
		resolved.UptermServer,
		resolved.StartupTimeout,
	); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrWorkflowDispatch, err)
	}

	run, err := waitForWorkflowRunStart(ctx, deadline, client, resolved.GitHubWorkflow, requestID, dispatchStartedAt)
	if err != nil {
		return nil, err
	}

	metadata, err := waitForMetadata(ctx, deadline, client, run.ID, requestID)
	if err != nil {
		return nil, err
	}

	if metadata.RequestID != "" && metadata.RequestID != requestID {
		return nil, fmt.Errorf(
			"%w: request_id mismatch, want %q got %q",
			ErrMetadataTimeout,
			requestID,
			metadata.RequestID,
		)
	}

	if metadata.SSHHost == "" || metadata.SSHUser == "" || metadata.SSHCommand == "" || metadata.SSHPort <= 0 {
		return nil, fmt.Errorf("%w: incomplete metadata", ErrMetadataTimeout)
	}

	createdAt := run.CreatedAt.Time
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	sbx := &Sandbox{
		ID:         requestID,
		Status:     firstNonEmpty(metadata.Status, run.EffectiveStatus()),
		Repository: resolved.GitHubRepository,
		Workflow:   resolved.GitHubWorkflow,
		Ref:        resolved.GitHubRef,
		RunID:      run.ID,
		RunURL:     run.HTMLURL,
		SSHHost:    metadata.SSHHost,
		SSHPort:    metadata.SSHPort,
		SSHUser:    metadata.SSHUser,
		SSHCommand: metadata.SSHCommand,
		CreatedAt:  createdAt,
		client:     client,
	}

	logger.Info().
		Str("request_id", requestID).
		Int64("run_id", run.ID).
		Str("ssh_command", sbx.SSHCommand).
		Msg("sandbox ready")

	return sbx, nil
}

// Close 取消对应的 workflow run，并等待 run 结束。
func (s *Sandbox) Close(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("%w: sandbox is nil", ErrInvalidOptions)
	}
	if strings.TrimSpace(s.Repository) == "" || s.RunID == 0 {
		return fmt.Errorf("%w: repository and run id are required", ErrInvalidOptions)
	}

	logger := zerolog.Ctx(ctx)

	client := s.client
	if client == nil {
		token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
		if token == "" {
			return fmt.Errorf("%w: missing GitHub token", ErrInvalidOptions)
		}
		client = newGitHubActionsClient(s.Repository, token)
	}

	logger.Info().Int64("run_id", s.RunID).Msg("canceling sandbox workflow run")

	if err := client.CancelWorkflowRun(ctx, s.RunID); err != nil {
		return err
	}

	run, err := waitForWorkflowRunCompletion(ctx, client, s.RunID)
	if err != nil {
		return err
	}

	s.Status = run.EffectiveStatus()
	if run.HTMLURL != "" {
		s.RunURL = run.HTMLURL
	}

	logger.Info().
		Int64("run_id", s.RunID).
		Str("status", s.Status).
		Msg("sandbox closed")

	return nil
}

func resolveCreateSandboxOptions(opts CreateSandboxOptions) (resolvedCreateSandboxOptions, error) {
	defaults := DefaultCreateSandboxOptions()

	resolved := resolvedCreateSandboxOptions{
		Name:             strings.TrimSpace(opts.Name),
		GitHubRepository: strings.TrimSpace(opts.GitHubRepository),
		GitHubWorkflow:   strings.TrimSpace(opts.GitHubWorkflow),
		GitHubRef:        strings.TrimSpace(opts.GitHubRef),
		GitHubToken:      strings.TrimSpace(opts.GitHubToken),
		UptermServer:     strings.TrimSpace(opts.UptermServer),
		StartupTimeout:   opts.StartupTimeout,
	}

	if resolved.GitHubWorkflow == "" {
		resolved.GitHubWorkflow = defaults.GitHubWorkflow
	}
	if resolved.GitHubRef == "" {
		resolved.GitHubRef = defaults.GitHubRef
	}
	if resolved.UptermServer == "" {
		resolved.UptermServer = defaults.UptermServer
	}
	if resolved.StartupTimeout == 0 {
		resolved.StartupTimeout = defaults.StartupTimeout
	}
	if resolved.GitHubToken == "" {
		resolved.GitHubToken = strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
	}

	if !githubactions.IsValidRepository(resolved.GitHubRepository) {
		return resolvedCreateSandboxOptions{}, fmt.Errorf("%w: invalid GitHub repository", ErrInvalidOptions)
	}
	if resolved.GitHubWorkflow == "" {
		return resolvedCreateSandboxOptions{}, fmt.Errorf("%w: missing GitHub workflow", ErrInvalidOptions)
	}
	if resolved.GitHubRef == "" {
		return resolvedCreateSandboxOptions{}, fmt.Errorf("%w: missing GitHub ref", ErrInvalidOptions)
	}
	if resolved.GitHubToken == "" {
		return resolvedCreateSandboxOptions{}, fmt.Errorf("%w: missing GitHub token", ErrInvalidOptions)
	}
	if resolved.UptermServer == "" {
		return resolvedCreateSandboxOptions{}, fmt.Errorf("%w: missing upterm server", ErrInvalidOptions)
	}
	if resolved.StartupTimeout <= 0 {
		return resolvedCreateSandboxOptions{}, fmt.Errorf("%w: startup timeout must be positive", ErrInvalidOptions)
	}

	return resolved, nil
}

func waitForWorkflowRunStart(
	ctx context.Context,
	deadline time.Time,
	client *githubactions.Client,
	workflow string,
	requestID string,
	dispatchStartedAt time.Time,
) (githubactions.WorkflowRun, error) {
	for {
		if err := ctx.Err(); err != nil {
			return githubactions.WorkflowRun{}, err
		}
		if time.Now().After(deadline) {
			return githubactions.WorkflowRun{}, fmt.Errorf("%w: request_id=%s", ErrWorkflowStartTimeout, requestID)
		}

		runs, err := client.ListWorkflowRuns(ctx, workflow, 20)
		if err != nil {
			return githubactions.WorkflowRun{}, err
		}

		run, found := findWorkflowRun(runs, requestID, dispatchStartedAt)
		if found {
			switch run.Status {
			case "in_progress":
				return run, nil
			case "completed":
				return githubactions.WorkflowRun{}, fmt.Errorf(
					"%w: run_id=%d conclusion=%s",
					ErrSandboxFailed,
					run.ID,
					firstNonEmpty(run.Conclusion, "unknown"),
				)
			}
		}

		if err := sleepWithContext(ctx, pollInterval); err != nil {
			return githubactions.WorkflowRun{}, err
		}
	}
}

func waitForMetadata(
	ctx context.Context,
	deadline time.Time,
	client *githubactions.Client,
	runID int64,
	requestID string,
) (sandboxMetadata, error) {
	artifactName := "sandbox-" + requestID

	for {
		if err := ctx.Err(); err != nil {
			return sandboxMetadata{}, err
		}
		if time.Now().After(deadline) {
			return sandboxMetadata{}, fmt.Errorf("%w: artifact=%s", ErrMetadataTimeout, artifactName)
		}

		artifacts, err := client.ListWorkflowArtifacts(ctx, runID)
		if err != nil {
			return sandboxMetadata{}, err
		}

		for _, item := range artifacts {
			if item.Expired || item.Name != artifactName {
				continue
			}
			payload, err := client.DownloadArtifactZIP(ctx, item.ID)
			if err != nil {
				return sandboxMetadata{}, err
			}
			return parseMetadataArchive(payload)
		}

		run, err := client.GetWorkflowRun(ctx, runID)
		if err != nil {
			return sandboxMetadata{}, err
		}
		if run.Status == "completed" {
			return sandboxMetadata{}, fmt.Errorf(
				"%w: run_id=%d conclusion=%s",
				ErrSandboxFailed,
				run.ID,
				firstNonEmpty(run.Conclusion, "unknown"),
			)
		}

		if err := sleepWithContext(ctx, pollInterval); err != nil {
			return sandboxMetadata{}, err
		}
	}
}

func waitForWorkflowRunCompletion(
	ctx context.Context,
	client *githubactions.Client,
	runID int64,
) (githubactions.WorkflowRun, error) {
	for {
		if err := ctx.Err(); err != nil {
			return githubactions.WorkflowRun{}, err
		}

		run, err := client.GetWorkflowRun(ctx, runID)
		if err != nil {
			return githubactions.WorkflowRun{}, err
		}
		if run.Status == "completed" {
			return run, nil
		}

		if err := sleepWithContext(ctx, pollInterval); err != nil {
			return githubactions.WorkflowRun{}, err
		}
	}
}

func findWorkflowRun(
	runs []githubactions.WorkflowRun,
	requestID string,
	dispatchStartedAt time.Time,
) (githubactions.WorkflowRun, bool) {
	for _, run := range runs {
		if runMatchesRequestID(run, requestID) {
			return run, true
		}
	}

	var candidate githubactions.WorkflowRun
	var found bool
	threshold := dispatchStartedAt.Add(-5 * time.Second)
	for _, run := range runs {
		if run.CreatedAt.Time.Before(threshold) {
			continue
		}
		if !found || run.CreatedAt.Time.After(candidate.CreatedAt.Time) || run.ID > candidate.ID {
			candidate = run
			found = true
		}
	}

	return candidate, found
}

func runMatchesRequestID(run githubactions.WorkflowRun, requestID string) bool {
	target := strings.ToLower(requestID)
	for _, field := range []string{run.DisplayTitle, run.RunName, run.Name} {
		if strings.Contains(strings.ToLower(field), target) {
			return true
		}
	}
	return false
}

func newGitHubActionsClient(repository string, token string) *githubactions.Client {
	return githubactions.NewClient(repository, token)
}

func parseMetadataArchive(data []byte) (sandboxMetadata, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return sandboxMetadata{}, err
	}

	var firstErr error
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}

		rc, err := file.Open()
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}

		payload, readErr := io.ReadAll(rc)
		closeErr := rc.Close()
		if readErr != nil {
			if firstErr == nil {
				firstErr = readErr
			}
			continue
		}
		if closeErr != nil && firstErr == nil {
			firstErr = closeErr
		}

		var metadata sandboxMetadata
		if err := json.Unmarshal(payload, &metadata); err == nil {
			return metadata, nil
		} else if firstErr == nil {
			firstErr = err
		}
	}

	if firstErr == nil {
		firstErr = errors.New("metadata archive is empty")
	}

	return sandboxMetadata{}, firstErr
}

func newRequestID(name string) string {
	prefix := sanitizeName(name)
	if prefix == "" {
		prefix = "sandbox"
	}

	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		now := time.Now().UTC().UnixNano()
		return fmt.Sprintf("%s-%d", prefix, now)
	}

	return fmt.Sprintf(
		"%s-%s-%s",
		prefix,
		time.Now().UTC().Format("20060102t150405"),
		hex.EncodeToString(buf[:]),
	)
}

func sanitizeName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return ""
	}

	var b strings.Builder
	lastDash := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteByte('-')
			lastDash = true
		}
	}

	return strings.Trim(b.String(), "-")
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
