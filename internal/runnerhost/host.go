package runnerhost

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

const (
	sshdevListenAddr = "127.0.0.1:2222"
	pinggyServerAddr = "a.pinggy.io"
	pinggyServerPort = "443"
)

var pinggyURLPattern = regexp.MustCompile(`tcp://[^[:space:]]+`)

// Options configures one helper run on the GitHub Actions runner.
type Options struct {
	RequestID      string
	PinggyToken    string
	MetadataPath   string
	StartupTimeout time.Duration
}

// Run starts sshdev, publishes Pinggy metadata, then blocks until the tunnel exits.
func Run(ctx context.Context, opts Options) error {
	if strings.TrimSpace(opts.RequestID) == "" {
		return errors.New("request id is required")
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

	sshUser := currentSSHUser()

	sshdevPath, err := exec.LookPath("sshdev")
	if err != nil {
		return fmt.Errorf("find sshdev: %w", err)
	}
	pinggyPath, err := exec.LookPath("ssh")
	if err != nil {
		return fmt.Errorf("find ssh: %w", err)
	}

	sshdevArgs := []string{
		"run",
		"--listen", sshdevListenAddr,
		"--shell", shell.Path,
	}
	sshdevCmd, sshdevWaitCh, streamErrCh, err := startLoggedCommand(
		ctx,
		sshdevPath,
		sshdevArgs,
		os.Environ(),
		nil,
	)
	if err != nil {
		return err
	}
	defer stopProcess(sshdevCmd)

	logger.Info().
		Str("request_id", opts.RequestID).
		Str("listen_addr", sshdevListenAddr).
		Str("shell", shell.Path).
		Msg("starting sshdev")

	if err := waitForTCPReady(ctx, sshdevWaitCh, sshdevListenAddr, opts.StartupTimeout); err != nil {
		return err
	}

	pinggyTarget := pinggyTarget(opts.PinggyToken)
	pinggyArgs := []string{
		"-p", pinggyServerPort,
		"-R0:localhost:2222",
		"-o", "BatchMode=yes",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "ServerAliveInterval=30",
		"-o", "ServerAliveCountMax=3",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		"-T",
		pinggyTarget,
	}

	parser := newPublicURLParser()
	pinggyCmd, pinggyWaitCh, pinggyStreamErrCh, err := startLoggedCommand(
		ctx,
		pinggyPath,
		pinggyArgs,
		os.Environ(),
		parser,
	)
	if err != nil {
		return err
	}
	defer stopProcess(pinggyCmd)
	streamErrCh = append(streamErrCh, pinggyStreamErrCh...)

	logger.Info().
		Str("request_id", opts.RequestID).
		Str("pinggy_target", pinggyTarget).
		Msg("starting pinggy tunnel")

	publicURL, err := waitForPublicURL(ctx, sshdevWaitCh, pinggyWaitCh, parser.Ready(), opts.StartupTimeout)
	if err != nil {
		return err
	}

	metadata, err := metadataFromPublicURL(opts.RequestID, sshUser, publicURL)
	if err != nil {
		return err
	}
	if err := writeMetadata(opts.MetadataPath, metadata); err != nil {
		return err
	}

	logger.Info().
		Str("metadata_path", opts.MetadataPath).
		Str("public_url", publicURL).
		Str("ssh_command", metadata.SSHCommand).
		Msg("sandbox metadata written")

	runErr := waitForHostedProcesses(ctx, sshdevWaitCh, pinggyWaitCh)
	for _, ch := range streamErrCh {
		if err := <-ch; err != nil && runErr == nil {
			runErr = err
		}
	}

	return runErr
}

func startLoggedCommand(
	ctx context.Context,
	path string,
	args []string,
	env []string,
	consumer lineConsumer,
) (*exec.Cmd, <-chan error, []<-chan error, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = env

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, nil, err
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	stdoutErrCh := make(chan error, 1)
	stderrErrCh := make(chan error, 1)
	go streamOutput(stdoutPipe, os.Stdout, consumer, stdoutErrCh)
	go streamOutput(stderrPipe, os.Stderr, consumer, stderrErrCh)

	return cmd, waitCh, []<-chan error{stdoutErrCh, stderrErrCh}, nil
}

func waitForTCPReady(
	ctx context.Context,
	waitCh <-chan error,
	addr string,
	timeout time.Duration,
) error {
	deadline := time.Now().Add(timeout)
	dialer := net.Dialer{Timeout: 500 * time.Millisecond}

	for {
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err == nil {
			_ = conn.Close()
			return nil
		}

		if err := ctx.Err(); err != nil {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for sshdev to listen on %s", addr)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-waitCh:
			if err == nil {
				return errors.New("sshdev exited before it started listening")
			}
			return fmt.Errorf("sshdev exited before it started listening: %w", err)
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func waitForPublicURL(
	ctx context.Context,
	sshdevWaitCh <-chan error,
	pinggyWaitCh <-chan error,
	publicURLCh <-chan string,
	timeout time.Duration,
) (string, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case err := <-sshdevWaitCh:
			if err == nil {
				return "", errors.New("sshdev exited before public url was published")
			}
			return "", fmt.Errorf("sshdev exited before public url was published: %w", err)
		case err := <-pinggyWaitCh:
			if err == nil {
				return "", errors.New("pinggy exited before public url was published")
			}
			return "", fmt.Errorf("pinggy exited before public url was published: %w", err)
		case publicURL := <-publicURLCh:
			return publicURL, nil
		case <-timer.C:
			return "", errors.New("timed out waiting for pinggy public url")
		}
	}
}

func waitForHostedProcesses(
	ctx context.Context,
	sshdevWaitCh <-chan error,
	pinggyWaitCh <-chan error,
) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-sshdevWaitCh:
		return unexpectedExit("sshdev", err)
	case err := <-pinggyWaitCh:
		return unexpectedExit("pinggy", err)
	}
}

func unexpectedExit(name string, err error) error {
	if err == nil {
		return fmt.Errorf("%s exited unexpectedly", name)
	}
	return fmt.Errorf("%s exited: %w", name, err)
}

func stopProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}

func pinggyTarget(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return "tcp@" + pinggyServerAddr
	}
	return token + "+tcp@" + pinggyServerAddr
}

func currentSSHUser() string {
	if current, err := user.Current(); err == nil {
		if value := strings.TrimSpace(current.Username); value != "" {
			return value
		}
	}

	for _, key := range []string{"USER", "LOGNAME"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}

	return "root"
}

type lineConsumer interface {
	Consume(string)
}

type publicURLParser struct {
	once    sync.Once
	readyCh chan string
}

func newPublicURLParser() *publicURLParser {
	return &publicURLParser{
		readyCh: make(chan string, 1),
	}
}

func (p *publicURLParser) Ready() <-chan string {
	return p.readyCh
}

func (p *publicURLParser) Consume(line string) {
	match := pinggyURLPattern.FindString(line)
	if match == "" {
		return
	}

	p.once.Do(func() {
		p.readyCh <- match
	})
}

func streamOutput(
	reader io.ReadCloser,
	writer io.Writer,
	consumer lineConsumer,
	errCh chan<- error,
) {
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		if consumer != nil {
			consumer.Consume(line)
		}
		if _, err := fmt.Fprintln(writer, line); err != nil {
			errCh <- err
			return
		}
	}

	errCh <- scanner.Err()
}
