package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/117503445/github-action-sandbox/sdk/sandbox"

	"github.com/rs/zerolog"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "basic example failed: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	if err := loadDotEnvIfPresent(".env"); err != nil {
		return err
	}

	logger := zerolog.New(os.Stderr).With().Timestamp().Logger()
	ctx := logger.WithContext(context.Background())

	repository := mustGetenv("GITHUB_REPOSITORY")
	token := mustGetenv("GITHUB_TOKEN")
	workflow := getenvDefault("GITHUB_WORKFLOW", "sandbox.yml")
	ref := getenvDefault("GITHUB_REF", "master")
	name := getenvDefault("SANDBOX_NAME", "basic-example")
	timeoutSeconds := getenvDefault("STARTUP_TIMEOUT_SECONDS", "300")
	cleanupTimeoutSeconds := getenvDefault("CLEANUP_TIMEOUT_SECONDS", "900")

	startupTimeout, err := time.ParseDuration(timeoutSeconds + "s")
	if err != nil {
		return fmt.Errorf("parse STARTUP_TIMEOUT_SECONDS: %w", err)
	}
	cleanupTimeout, err := time.ParseDuration(cleanupTimeoutSeconds + "s")
	if err != nil {
		return fmt.Errorf("parse CLEANUP_TIMEOUT_SECONDS: %w", err)
	}

	item, err := sandbox.CreateSandbox(ctx, sandbox.CreateSandboxOptions{
		Name:             name,
		GitHubRepository: repository,
		GitHubWorkflow:   workflow,
		GitHubRef:        ref,
		GitHubToken:      token,
		StartupTimeout:   startupTimeout,
	})
	if err != nil {
		return err
	}

	fmt.Printf("sandbox ready: %s\n", item.RunURL)
	fmt.Printf("ssh command: %s\n", item.SSHCommand)

	listCtx, listCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer listCancel()

	listed, err := verifyListAPI(listCtx, repository, workflow, token, item)
	if err != nil {
		return err
	}

	fmt.Printf("list verification passed: run=%d listed=%d ssh=%s\n", item.RunID, len(listed), item.SSHCommand)

	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer closeCancel()

		if err := item.Close(closeCtx); err != nil {
			fmt.Fprintf(os.Stderr, "close sandbox failed: %v\n", err)
			return
		}
		fmt.Printf("sandbox closed: %s (%s)\n", item.RunURL, item.Status)
	}()

	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cleanupCancel()

	cleanupOpts := sandbox.DefaultFreeDiskSpaceOptions()
	cleanupReport, err := item.FreeDiskSpace(cleanupCtx, cleanupOpts)
	if err != nil {
		return fmt.Errorf("free disk space: %w", err)
	}

	fmt.Printf(
		"disk cleanup passed: before=%dB after=%dB freed=%dB duration=%s\n",
		cleanupReport.AvailableBytesBefore,
		cleanupReport.AvailableBytesAfter,
		cleanupReport.FreedBytes,
		cleanupReport.Duration,
	)
	for _, step := range cleanupReport.Steps {
		fmt.Printf(
			"  - %s: start=%s end=%s duration=%s before=%dB after=%dB freed=%dB\n",
			step.Name,
			step.StartedAt.Format(time.RFC3339),
			step.CompletedAt.Format(time.RFC3339),
			step.Duration,
			step.AvailableBytesBefore,
			step.AvailableBytesAfter,
			step.FreedBytes,
		)
	}

	return nil
}

func verifyListAPI(
	ctx context.Context,
	repository string,
	workflow string,
	token string,
	created *sandbox.Sandbox,
) ([]*sandbox.Sandbox, error) {
	items, err := sandbox.ListSandboxes(ctx, sandbox.ListSandboxesOptions{
		GitHubRepository: repository,
		GitHubWorkflow:   workflow,
		GitHubToken:      token,
		Limit:            10,
	})
	if err != nil {
		return nil, fmt.Errorf("list sandboxes: %w", err)
	}

	for _, item := range items {
		if item.RunID != created.RunID {
			continue
		}
		if item.ID != created.ID {
			return nil, fmt.Errorf("list returned mismatched request id: got %q want %q", item.ID, created.ID)
		}
		if item.SSHCommand != created.SSHCommand {
			return nil, fmt.Errorf("list returned mismatched ssh command: got %q want %q", item.SSHCommand, created.SSHCommand)
		}
		return items, nil
	}

	return nil, fmt.Errorf("created sandbox run %d was not returned by list api", created.RunID)
}

func verifySSH(ctx context.Context, item *sandbox.Sandbox) (string, error) {
	args := []string{
		"-o", "StrictHostKeyChecking=accept-new",
		"-tt",
		"-p", strconv.Itoa(item.SSHPort),
		item.SSHUser + "@" + item.SSHHost,
	}
	cmd := exec.CommandContext(ctx, "ssh", args...)

	var output lockedBuffer

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", err
	}

	if err := cmd.Start(); err != nil {
		return "", err
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	streamErrCh := make(chan error, 2)
	go streamSSHOutput(stdoutPipe, io.MultiWriter(os.Stdout, &output), streamErrCh)
	go streamSSHOutput(stderrPipe, io.MultiWriter(os.Stderr, &output), streamErrCh)

	proc := sshProcessState{waitCh: waitCh}

	if err := waitForSSHSessionReady(ctx, &output, &proc, 2*time.Second); err != nil {
		_ = stdin.Close()
		_ = proc.Wait()
		normalized := cleanTerminalOutput(output.String())
		_ = waitForOutputStreams(streamErrCh, 2)
		return sanitizeOutput(normalized), err
	}

	if _, err := io.WriteString(stdin, verificationScript()); err != nil {
		_ = stdin.Close()
		return "", err
	}

	result, err := waitForSSHProbeResult(ctx, &output, &proc)
	if _, writeErr := io.WriteString(stdin, "exit\n"); writeErr != nil && err == nil {
		err = writeErr
	}
	_ = stdin.Close()

	waitErr := proc.Wait()
	normalized := cleanTerminalOutput(output.String())
	streamErr := waitForOutputStreams(streamErrCh, 2)
	sanitized := sanitizeOutput(normalized)

	if err != nil {
		if streamErr != nil {
			return sanitized, fmt.Errorf("%w: %v", err, streamErr)
		}
		if waitErr != nil {
			return sanitized, fmt.Errorf("%w: %v", err, waitErr)
		}
		return sanitized, err
	}

	if streamErr != nil {
		return sanitized, streamErr
	}

	if err := validateSSHProbeResult(item.Repository, result); err != nil {
		return sanitized, err
	}

	return sanitized, nil
}

func verificationScript() string {
	return "\nprintf '" + sshProbeMarker + " %s %s\\n' \"$(id -un)\" \"$PWD\"\n"
}

type sshProbeResult struct {
	User string
	PWD  string
}

func extractSSHProbeResult(output string) (sshProbeResult, bool) {
	matches := sshProbePattern.FindAllStringSubmatch(output, -1)
	if len(matches) > 0 {
		lastMatch := matches[len(matches)-1]
		if len(lastMatch) == 3 {
			return sshProbeResult{
				User: lastMatch[1],
				PWD:  lastMatch[2],
			}, true
		}
	}

	return sshProbeResult{}, false
}

func validateSSHProbeResult(repository string, result sshProbeResult) error {
	if result.User != "root" {
		return fmt.Errorf("ssh reached unexpected user %q", result.User)
	}

	expectedWorkspace := expectedRunnerWorkspace(repository)
	if !strings.Contains(result.PWD, expectedWorkspace) {
		return fmt.Errorf("ssh output did not contain runner workspace: got %q want %q", result.PWD, expectedWorkspace)
	}

	return nil
}

func expectedRunnerWorkspace(repository string) string {
	repoName := path.Base(strings.TrimSpace(repository))
	return path.Join("/home/runner/work", repoName, repoName)
}

func sanitizeOutput(output string) string {
	lines := strings.Split(output, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		filtered = append(filtered, line)
	}
	return strings.Join(filtered, "\n")
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *lockedBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Len()
}

type sshProcessState struct {
	waitCh <-chan error
	done   bool
	err    error
}

func (p *sshProcessState) Poll() (bool, error) {
	if p.done {
		return true, p.err
	}

	select {
	case err := <-p.waitCh:
		p.done = true
		p.err = err
		return true, err
	default:
		return false, nil
	}
}

func (p *sshProcessState) Wait() error {
	if p.done {
		return p.err
	}

	p.err = <-p.waitCh
	p.done = true
	return p.err
}

func waitForSSHSessionReady(
	ctx context.Context,
	output *lockedBuffer,
	proc *sshProcessState,
	grace time.Duration,
) error {
	timer := time.NewTimer(grace)
	defer timer.Stop()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		if output.Len() > 0 {
			return nil
		}

		if done, err := proc.Poll(); done {
			if err != nil {
				return fmt.Errorf("ssh exited before session became ready: %w", err)
			}
			return errors.New("ssh exited before session became ready")
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		case <-ticker.C:
		}
	}
}

func waitForSSHProbeResult(
	ctx context.Context,
	output *lockedBuffer,
	proc *sshProcessState,
) (sshProbeResult, error) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		normalized := cleanTerminalOutput(output.String())
		if result, ok := extractSSHProbeResult(normalized); ok {
			return result, nil
		}

		if done, err := proc.Poll(); done {
			if err != nil {
				return sshProbeResult{}, fmt.Errorf("ssh did not emit verification marker: %w", err)
			}
			return sshProbeResult{}, errors.New("ssh did not emit verification marker")
		}

		select {
		case <-ctx.Done():
			return sshProbeResult{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func streamSSHOutput(reader io.ReadCloser, writer io.Writer, errCh chan<- error) {
	defer reader.Close()
	_, err := io.Copy(writer, reader)
	errCh <- err
}

func waitForOutputStreams(errCh <-chan error, count int) error {
	var firstErr error
	for i := 0; i < count; i++ {
		if err := <-errCh; err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

var (
	ansiCSI         = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
	ansiOSC         = regexp.MustCompile(`\x1b\][^\a\x1b]*(?:\a|\x1b\\)`)
	sshProbePattern = regexp.MustCompile(regexp.QuoteMeta(sshProbeMarker) + ` ([^%[:space:]][^[:space:]]*) (/[^\r\n]+)`)
)

const sshProbeMarker = "__GAS_PROBE__"

func cleanTerminalOutput(output string) string {
	output = strings.ReplaceAll(output, "\r", "\n")
	output = ansiOSC.ReplaceAllString(output, "")
	output = ansiCSI.ReplaceAllString(output, "")
	output = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r < 32 {
			return -1
		}
		return r
	}, output)
	return output
}

func loadDotEnvIfPresent(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read %s: %w", path, err)
	}

	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("invalid .env line: %q", rawLine)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}

	return nil
}

func mustGetenv(key string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		panic("missing env: " + key)
	}
	return value
}

func getenvDefault(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}
