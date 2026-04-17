package runnerhost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// Options configures one helper run on the GitHub Actions runner.
type Options struct {
	RequestID      string
	UptermServer   string
	MetadataPath   string
	StartupTimeout time.Duration
}

// Run starts upterm, writes metadata, then blocks until the hosted session exits.
func Run(ctx context.Context, opts Options) error {
	if strings.TrimSpace(opts.RequestID) == "" {
		return errors.New("request id is required")
	}
	if strings.TrimSpace(opts.UptermServer) == "" {
		return errors.New("upterm server is required")
	}
	if strings.TrimSpace(opts.MetadataPath) == "" {
		return errors.New("metadata path is required")
	}
	if opts.StartupTimeout <= 0 {
		return errors.New("startup timeout must be positive")
	}

	logger := zerolog.Ctx(ctx)

	shell, err := SelectShell()
	if err != nil {
		return err
	}

	uptermPath, err := exec.LookPath("upterm")
	if err != nil {
		return fmt.Errorf("find upterm: %w", err)
	}

	socketDir := filepath.Join(userHomeDir(), ".upterm")
	before, err := listSocketCandidates(socketDir)
	if err != nil {
		return err
	}

	args := []string{
		"host",
		"--accept",
		"--skip-host-key-check",
		"--server", opts.UptermServer,
		"--",
	}
	args = append(args, shell.Command()...)

	cmd := exec.CommandContext(ctx, uptermPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	logger.Info().
		Str("request_id", opts.RequestID).
		Str("upterm_server", opts.UptermServer).
		Str("shell", strings.Join(shell.Command(), " ")).
		Msg("starting sandbox host")

	if err := cmd.Start(); err != nil {
		return err
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	socketPath, err := waitForSocket(ctx, waitCh, socketDir, before, opts.StartupTimeout)
	if err != nil {
		return err
	}

	session, err := waitForCurrentSession(ctx, uptermPath, socketPath, opts.StartupTimeout)
	if err != nil {
		return err
	}

	metadata, err := metadataFromSession(opts.RequestID, session)
	if err != nil {
		return err
	}
	if err := writeMetadata(opts.MetadataPath, metadata); err != nil {
		return err
	}

	logger.Info().
		Str("metadata_path", opts.MetadataPath).
		Str("ssh_command", metadata.SSHCommand).
		Msg("sandbox metadata written")

	return <-waitCh
}

func waitForSocket(
	ctx context.Context,
	waitCh <-chan error,
	socketDir string,
	before map[string]struct{},
	timeout time.Duration,
) (string, error) {
	deadline := time.Now().Add(timeout)

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case err := <-waitCh:
			if err == nil {
				return "", errors.New("upterm exited before session socket was ready")
			}
			return "", err
		default:
		}

		if time.Now().After(deadline) {
			return "", errors.New("timed out waiting for upterm admin socket")
		}

		after, err := listSocketCandidates(socketDir)
		if err != nil {
			return "", err
		}
		for path := range after {
			if _, ok := before[path]; !ok {
				return path, nil
			}
		}

		if err := sleepWithContext(ctx, 200*time.Millisecond); err != nil {
			return "", err
		}
	}
}

func waitForCurrentSession(
	ctx context.Context,
	uptermPath string,
	socketPath string,
	timeout time.Duration,
) (currentSession, error) {
	deadline := time.Now().Add(timeout)

	for {
		if err := ctx.Err(); err != nil {
			return currentSession{}, err
		}
		if time.Now().After(deadline) {
			return currentSession{}, errors.New("timed out waiting for upterm session metadata")
		}

		cmd := exec.CommandContext(ctx, uptermPath, "session", "current", "--admin-socket", socketPath, "-o", "json")
		output, err := cmd.Output()
		if err == nil {
			var session currentSession
			if err := json.Unmarshal(output, &session); err != nil {
				return currentSession{}, err
			}
			return session, nil
		}

		if err := sleepWithContext(ctx, 200*time.Millisecond); err != nil {
			return currentSession{}, err
		}
	}
}

func listSocketCandidates(dir string) (map[string]struct{}, error) {
	entries, err := filepath.Glob(filepath.Join(dir, "*.sock"))
	if err != nil {
		return nil, err
	}

	out := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		out[entry] = struct{}{}
	}
	return out, nil
}

func userHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "/root"
	}
	return home
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
