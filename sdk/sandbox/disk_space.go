package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

const availableBytesCommand = "LC_ALL=C df -B1 -a --output=avail 2>/dev/null | awk 'NR > 1 {sum += $1} END {print sum + 0}'"
const availableBytesMarker = "__GAS_AVAILABLE__"
const availableBytesBeforeMarker = "__GAS_BEFORE__"
const availableBytesAfterMarker = "__GAS_AFTER__"

const availableBytesScript = "value=$(" + availableBytesCommand + "); printf '%s %s\\n' '" + availableBytesMarker + "' \"$value\""

type diskCleanupSpec struct {
	Name    string
	Command string
}

type sshCommandOutput struct {
	Stdout string
	Stderr string
}

var sshScriptRunner = defaultSSHScriptRunner

var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// FreeDiskSpace 基于 SSH 在 sandbox 内执行磁盘清理，并返回每个步骤的耗时和释放空间。
func (s *Sandbox) FreeDiskSpace(ctx context.Context, opts FreeDiskSpaceOptions) (*FreeDiskSpaceResult, error) {
	if err := s.validateSSHAccess(); err != nil {
		return nil, err
	}

	logger := zerolog.Ctx(ctx)
	steps := buildDiskCleanupSpecs(opts)

	startedAt := time.Now().UTC()
	result := &FreeDiskSpaceResult{
		StartedAt: startedAt,
		Steps:     make([]DiskCleanupStep, 0, len(steps)),
	}

	if len(steps) == 0 {
		currentBytes, err := s.availableBytes(ctx)
		if err != nil {
			return nil, err
		}

		result.CompletedAt = time.Now().UTC()
		result.Duration = result.CompletedAt.Sub(result.StartedAt)
		result.AvailableBytesBefore = currentBytes
		result.AvailableBytesAfter = currentBytes
		return result, nil
	}

	var currentBytes int64

	for _, spec := range steps {
		step := DiskCleanupStep{
			Name:    spec.Name,
			Command: spec.Command,
		}

		logger.Info().Str("sandbox_id", s.ID).Str("step", spec.Name).Msg("starting disk cleanup step")

		step.StartedAt = time.Now().UTC()
		beforeBytes, afterBytes, err := s.runMeasuredDiskCleanupStep(ctx, spec.Command)
		if err != nil {
			return nil, fmt.Errorf("%w: step %q: %v", ErrSSHExecution, spec.Name, err)
		}
		step.CompletedAt = time.Now().UTC()
		step.Duration = step.CompletedAt.Sub(step.StartedAt)

		step.AvailableBytesBefore = beforeBytes
		step.AvailableBytesAfter = afterBytes
		step.FreedBytes = step.AvailableBytesAfter - step.AvailableBytesBefore

		if len(result.Steps) == 0 {
			result.AvailableBytesBefore = beforeBytes
		}
		currentBytes = afterBytes
		result.Steps = append(result.Steps, step)

		logger.Info().
			Str("sandbox_id", s.ID).
			Str("step", spec.Name).
			Dur("duration", step.Duration).
			Int64("freed_bytes", step.FreedBytes).
			Msg("completed disk cleanup step")
	}

	result.CompletedAt = time.Now().UTC()
	result.Duration = result.CompletedAt.Sub(result.StartedAt)
	result.AvailableBytesAfter = currentBytes
	result.FreedBytes = result.AvailableBytesAfter - result.AvailableBytesBefore

	return result, nil
}

func (s *Sandbox) validateSSHAccess() error {
	if s == nil {
		return fmt.Errorf("%w: sandbox is nil", ErrInvalidOptions)
	}
	if strings.TrimSpace(s.SSHHost) == "" || strings.TrimSpace(s.SSHUser) == "" || s.SSHPort <= 0 {
		return fmt.Errorf("%w: ssh host, port, and user are required", ErrInvalidOptions)
	}

	return nil
}

func buildDiskCleanupSpecs(opts FreeDiskSpaceOptions) []diskCleanupSpec {
	specs := make([]diskCleanupSpec, 0, 7)

	if opts.Android {
		specs = append(specs, diskCleanupSpec{
			Name: "android",
			Command: strings.Join([]string{
				"set -e",
				"sudo -n rm -rf /usr/local/lib/android || true",
			}, "\n"),
		})
	}

	if opts.Dotnet {
		specs = append(specs, diskCleanupSpec{
			Name: "dotnet",
			Command: strings.Join([]string{
				"set -e",
				"sudo -n rm -rf /usr/share/dotnet || true",
			}, "\n"),
		})
	}

	if opts.Haskell {
		specs = append(specs, diskCleanupSpec{
			Name: "haskell",
			Command: strings.Join([]string{
				"set -e",
				"sudo -n rm -rf /opt/ghc || true",
				"sudo -n rm -rf /usr/local/.ghcup || true",
			}, "\n"),
		})
	}

	if opts.LargePackages {
		specs = append(specs, diskCleanupSpec{
			Name: "large-packages",
			Command: strings.Join([]string{
				"set -e",
				"export DEBIAN_FRONTEND=noninteractive",
				`sudo -n apt-get remove -y '^aspnetcore-.*' || echo "aspnetcore packages not present"`,
				`sudo -n apt-get remove -y '^dotnet-.*' --fix-missing || echo "dotnet packages not present"`,
				`sudo -n apt-get remove -y '^llvm-.*' --fix-missing || echo "llvm packages not present"`,
				`sudo -n apt-get remove -y 'php.*' --fix-missing || echo "php packages not present"`,
				`sudo -n apt-get remove -y '^mongodb-.*' --fix-missing || echo "mongodb packages not present"`,
				`sudo -n apt-get remove -y '^mysql-.*' --fix-missing || echo "mysql packages not present"`,
				"sudo -n apt-get remove -y azure-cli google-chrome-stable firefox powershell mono-devel libgl1-mesa-dri --fix-missing || echo \"large packages already absent\"",
				"sudo -n apt-get remove -y google-cloud-sdk --fix-missing || echo \"google-cloud-sdk not present\"",
				"sudo -n apt-get remove -y google-cloud-cli --fix-missing || echo \"google-cloud-cli not present\"",
				"sudo -n apt-get autoremove -y || true",
				"sudo -n apt-get clean || true",
			}, "\n"),
		})
	}

	if opts.DockerImages {
		specs = append(specs, diskCleanupSpec{
			Name: "docker-images",
			Command: strings.Join([]string{
				"set -e",
				"sudo -n docker image prune --all --force || true",
			}, "\n"),
		})
	}

	if opts.ToolCache {
		specs = append(specs, diskCleanupSpec{
			Name: "tool-cache",
			Command: strings.Join([]string{
				"set -e",
				"if [ -n \"${AGENT_TOOLSDIRECTORY:-}\" ]; then sudo -n rm -rf \"$AGENT_TOOLSDIRECTORY\" || true; fi",
			}, "\n"),
		})
	}

	if opts.SwapStorage {
		specs = append(specs, diskCleanupSpec{
			Name: "swap-storage",
			Command: strings.Join([]string{
				"set -e",
				"sudo -n swapoff -a || true",
				"sudo -n rm -f /mnt/swapfile || true",
			}, "\n"),
		})
	}

	return specs
}

func (s *Sandbox) availableBytes(ctx context.Context) (int64, error) {
	output, err := s.runSSHScript(ctx, availableBytesScript)
	if err != nil {
		return 0, err
	}

	value, err := parseMarkedInt64FromOutput(combinedSSHOutput(output), availableBytesMarker)
	if err != nil {
		return 0, fmt.Errorf("%w: parse available bytes: %v", ErrSSHExecution, err)
	}

	return value, nil
}

func (s *Sandbox) runMeasuredDiskCleanupStep(ctx context.Context, command string) (int64, int64, error) {
	script := strings.Join([]string{
		"before=$(" + availableBytesCommand + ")",
		"printf '%s %s\\n' '" + availableBytesBeforeMarker + "' \"$before\"",
		command,
		"after=$(" + availableBytesCommand + ")",
		"printf '%s %s\\n' '" + availableBytesAfterMarker + "' \"$after\"",
	}, "\n")

	output, err := s.runSSHScript(ctx, script)
	if err != nil {
		return 0, 0, err
	}

	combined := combinedSSHOutput(output)
	beforeBytes, err := parseMarkedInt64FromOutput(combined, availableBytesBeforeMarker)
	if err != nil {
		return 0, 0, fmt.Errorf("parse before bytes: %w; output=%q", err, truncateForError(normalizeSSHOutput(combined), 800))
	}
	afterBytes, err := parseMarkedInt64FromOutput(combined, availableBytesAfterMarker)
	if err != nil {
		return 0, 0, fmt.Errorf("parse after bytes: %w; output=%q", err, truncateForError(normalizeSSHOutput(combined), 800))
	}

	return beforeBytes, afterBytes, nil
}

func (s *Sandbox) runSSHScript(ctx context.Context, script string) (sshCommandOutput, error) {
	return sshScriptRunner(ctx, s, script)
}

func defaultSSHScriptRunner(ctx context.Context, s *Sandbox, script string) (sshCommandOutput, error) {
	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		return sshCommandOutput{}, fmt.Errorf("%w: find ssh: %v", ErrSSHExecution, err)
	}

	args := []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		"-tt",
		"-p", strconv.Itoa(s.SSHPort),
		fmt.Sprintf("%s@%s", s.SSHUser, s.SSHHost),
	}

	cmd := exec.CommandContext(ctx, sshPath, args...)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return sshCommandOutput{}, fmt.Errorf("%w: stdout pipe: %v", ErrSSHExecution, err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return sshCommandOutput{}, fmt.Errorf("%w: stderr pipe: %v", ErrSSHExecution, err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return sshCommandOutput{}, fmt.Errorf("%w: stdin pipe: %v", ErrSSHExecution, err)
	}

	if err := cmd.Start(); err != nil {
		return sshCommandOutput{}, fmt.Errorf("%w: start ssh: %v", ErrSSHExecution, err)
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	var combined lockedBuffer
	var stdout lockedBuffer
	var stderr lockedBuffer
	streamErrCh := make(chan error, 2)
	go streamSSHOutput(stdoutPipe, io.MultiWriter(&stdout, &combined), streamErrCh)
	go streamSSHOutput(stderrPipe, io.MultiWriter(&stderr, &combined), streamErrCh)

	proc := sshProcessState{waitCh: waitCh}
	if err := waitForSSHSessionReady(ctx, &combined, &proc, 2*time.Second); err != nil {
		_ = stdin.Close()
		_ = proc.Wait()
		_ = waitForOutputStreams(streamErrCh, 2)
		return sshCommandOutput{}, fmt.Errorf("%w: %v", ErrSSHExecution, err)
	}

	doneMarker := fmt.Sprintf("__GAS_DONE__%d", time.Now().UnixNano())
	payload := "\n" + script + "\nprintf '" + doneMarker + "\\n'\n"
	if _, err := io.WriteString(stdin, payload); err != nil {
		_ = stdin.Close()
		_ = proc.Wait()
		_ = waitForOutputStreams(streamErrCh, 2)
		return sshCommandOutput{}, fmt.Errorf("%w: write ssh script: %v", ErrSSHExecution, err)
	}

	if err := waitForSSHLineMarker(ctx, &combined, &proc, doneMarker); err != nil {
		_ = stdin.Close()
		_ = proc.Wait()
		_ = waitForOutputStreams(streamErrCh, 2)
		return sshCommandOutput{}, fmt.Errorf("%w: %v", ErrSSHExecution, err)
	}

	_ = stdin.Close()
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	_ = proc.Wait()
	if err := waitForOutputStreams(streamErrCh, 2); err != nil {
		return sshCommandOutput{}, fmt.Errorf("%w: %v", ErrSSHExecution, err)
	}

	return sshCommandOutput{Stdout: stdout.String(), Stderr: stderr.String()}, nil
}

func parseInt64FromOutput(value string) (int64, error) {
	normalized := normalizeSSHOutput(value)
	lines := strings.Split(strings.TrimSpace(normalized), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}

		if strings.Contains(line, availableBytesMarker) {
			suffix := strings.TrimSpace(line[strings.Index(line, availableBytesMarker)+len(availableBytesMarker):])
			for _, field := range strings.Fields(suffix) {
				parsed, err := strconv.ParseInt(strings.TrimSpace(field), 10, 64)
				if err == nil {
					return parsed, nil
				}
			}
		}

		if parsed, err := strconv.ParseInt(line, 10, 64); err == nil {
			return parsed, nil
		}

		fields := strings.Fields(line)
		for j := len(fields) - 1; j >= 0; j-- {
			field := strings.TrimSpace(fields[j])
			if field == "" {
				continue
			}

			parsed, err := strconv.ParseInt(field, 10, 64)
			if err == nil {
				return parsed, nil
			}
		}
	}

	return 0, fmt.Errorf("empty output")
}

func parseMarkedInt64FromOutput(value string, marker string) (int64, error) {
	normalized := normalizeSSHOutput(value)
	lines := strings.Split(strings.TrimSpace(normalized), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if !strings.Contains(line, marker) {
			continue
		}

		suffix := strings.TrimSpace(line[strings.Index(line, marker)+len(marker):])
		for _, field := range strings.Fields(suffix) {
			parsed, err := strconv.ParseInt(strings.TrimSpace(field), 10, 64)
			if err == nil {
				return parsed, nil
			}
		}
	}

	return 0, fmt.Errorf("marker %q not found", marker)
}

func normalizeSSHOutput(value string) string {
	normalized := ansiEscapePattern.ReplaceAllString(value, "")
	return strings.ReplaceAll(normalized, "\r", "\n")
}

func combinedSSHOutput(output sshCommandOutput) string {
	if strings.TrimSpace(output.Stderr) == "" {
		return output.Stdout
	}
	if strings.TrimSpace(output.Stdout) == "" {
		return output.Stderr
	}
	return output.Stdout + "\n" + output.Stderr
}

func truncateForError(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	return value[:max-3] + "..."
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

func waitForSSHLineMarker(
	ctx context.Context,
	output *lockedBuffer,
	proc *sshProcessState,
	marker string,
) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		if hasExactOutputLine(cleanTerminalOutput(output.String()), marker) {
			return nil
		}

		if done, err := proc.Poll(); done {
			if err != nil {
				return fmt.Errorf("ssh did not emit completion marker: %w", err)
			}
			return errors.New("ssh did not emit completion marker")
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func hasExactOutputLine(output string, marker string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == marker {
			return true
		}
	}
	return false
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

func cleanTerminalOutput(output string) string {
	output = normalizeSSHOutput(output)
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r < 32 {
			return -1
		}
		return r
	}, output)
}
