package runnerhost

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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

	privateKeyPath, cleanupKey, err := generatePrivateKey(ctx)
	if err != nil {
		return err
	}
	defer cleanupKey()

	args := []string{
		"host",
		"--accept",
		"--skip-host-key-check",
		"--private-key", privateKeyPath,
		"--server", opts.UptermServer,
		"--",
	}
	args = append(args, shell.Command()...)

	cmd := exec.CommandContext(ctx, uptermPath, args...)
	cmd.Env = os.Environ()

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

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

	parser := newSessionOutputParser()
	streamErrCh := make(chan error, 2)
	go streamOutput(stdoutPipe, os.Stdout, parser, streamErrCh)
	go streamOutput(stderrPipe, os.Stderr, parser, streamErrCh)

	session, err := waitForSession(ctx, waitCh, parser.Ready(), opts.StartupTimeout)
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

	runErr := <-waitCh

	for i := 0; i < 2; i++ {
		if err := <-streamErrCh; err != nil && runErr == nil {
			runErr = err
		}
	}

	return runErr
}

func waitForSession(
	ctx context.Context,
	waitCh <-chan error,
	sessionCh <-chan currentSession,
	timeout time.Duration,
) (currentSession, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return currentSession{}, ctx.Err()
		case err := <-waitCh:
			if err == nil {
				return currentSession{}, errors.New("upterm exited before session info was published")
			}
			return currentSession{}, err
		case session := <-sessionCh:
			return session, nil
		case <-timer.C:
			return currentSession{}, errors.New("timed out waiting for upterm session metadata")
		}
	}
}

type sessionOutputParser struct {
	mu      sync.Mutex
	once    sync.Once
	session currentSession
	readyCh chan currentSession
}

func newSessionOutputParser() *sessionOutputParser {
	return &sessionOutputParser{
		readyCh: make(chan currentSession, 1),
	}
}

func (p *sessionOutputParser) Ready() <-chan currentSession {
	return p.readyCh
}

func (p *sessionOutputParser) Consume(line string) {
	line = strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(line, "Session:"):
		p.mu.Lock()
		p.session.SessionID = strings.TrimSpace(strings.TrimPrefix(line, "Session:"))
		session := p.session
		p.mu.Unlock()
		p.maybePublish(session)
	case strings.HasPrefix(line, "Host:"):
		p.mu.Lock()
		p.session.Host = strings.TrimSpace(strings.TrimPrefix(line, "Host:"))
		session := p.session
		p.mu.Unlock()
		p.maybePublish(session)
	case strings.HasPrefix(line, "Command:"):
		p.mu.Lock()
		p.session.Command = strings.TrimSpace(strings.TrimPrefix(line, "Command:"))
		p.mu.Unlock()
	}
}

func (p *sessionOutputParser) maybePublish(session currentSession) {
	if session.SessionID == "" || session.Host == "" {
		return
	}

	p.once.Do(func() {
		p.readyCh <- session
	})
}

func streamOutput(
	reader io.ReadCloser,
	writer io.Writer,
	parser *sessionOutputParser,
	errCh chan<- error,
) {
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		parser.Consume(line)
		if _, err := fmt.Fprintln(writer, line); err != nil {
			errCh <- err
			return
		}
	}

	errCh <- scanner.Err()
}

func generatePrivateKey(ctx context.Context) (string, func(), error) {
	sshKeygenPath, err := exec.LookPath("ssh-keygen")
	if err != nil {
		return "", nil, fmt.Errorf("find ssh-keygen: %w", err)
	}

	dir, err := os.MkdirTemp("", "sandbox-host-key-*")
	if err != nil {
		return "", nil, err
	}

	keyPath := filepath.Join(dir, "upterm-host-key")
	cmd := exec.CommandContext(ctx, sshKeygenPath, "-q", "-t", "ed25519", "-N", "", "-f", keyPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", nil, fmt.Errorf("generate private key: %w: %s", err, strings.TrimSpace(string(output)))
	}

	return keyPath, func() {
		_ = os.RemoveAll(dir)
	}, nil
}
