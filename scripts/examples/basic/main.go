package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/117503445/github-action-sandbox/sdk/sandbox"
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

	startupTimeout, err := envDurationSeconds("STARTUP_TIMEOUT_SECONDS", 300)
	if err != nil {
		return err
	}
	cleanupTimeout, err := envDurationSeconds("CLEANUP_TIMEOUT_SECONDS", 900)
	if err != nil {
		return err
	}

	opts := sandbox.DefaultCreateSandboxOptions()
	opts.Name = envOr("SANDBOX_NAME", "basic-example")
	opts.GitHubRepository = mustEnv("GITHUB_REPOSITORY")
	opts.GitHubWorkflow = envOr("GITHUB_WORKFLOW", opts.GitHubWorkflow)
	opts.GitHubRef = envOr("GITHUB_REF", opts.GitHubRef)
	opts.GitHubToken = mustEnv("GITHUB_TOKEN")
	opts.PinggyToken = strings.TrimSpace(os.Getenv("PINGGY_TOKEN"))
	opts.StartupTimeout = startupTimeout

	item, err := sandbox.CreateSandbox(context.Background(), opts)
	if err != nil {
		return fmt.Errorf("create sandbox: %w", err)
	}
	defer closeSandbox(item)

	fmt.Printf("run: %s\n", item.RunURL)
	fmt.Printf("ssh: %s\n", item.SSHCommand)

	items, err := sandbox.ListSandboxes(context.Background(), sandbox.ListSandboxesOptions{
		GitHubRepository: opts.GitHubRepository,
		GitHubWorkflow:   opts.GitHubWorkflow,
		GitHubToken:      opts.GitHubToken,
		Limit:            10,
	})
	if err != nil {
		return fmt.Errorf("list sandboxes: %w", err)
	}
	if !containsSandbox(items, item.RunID, item.SSHCommand) {
		return fmt.Errorf("run %d not found in recent sandboxes", item.RunID)
	}
	fmt.Printf("list: ok (%d items)\n", len(items))

	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cleanupCancel()

	report, err := item.FreeDiskSpace(cleanupCtx, sandbox.DefaultFreeDiskSpaceOptions())
	if err != nil {
		return fmt.Errorf("free disk space: %w", err)
	}

	fmt.Printf(
		"cleanup: available_before=%s available_after=%s freed=%s speed=%s duration=%s\n",
		formatBytes(report.AvailableBytesBefore),
		formatBytes(report.AvailableBytesAfter),
		formatBytes(report.FreedBytes),
		formatSpeed(report.FreedBytes, report.Duration),
		formatDuration(report.Duration),
	)
	for _, step := range report.Steps {
		fmt.Printf(
			"- %s: freed=%s speed=%s duration=%s\n",
			step.Name,
			formatBytes(step.FreedBytes),
			formatSpeed(step.FreedBytes, step.Duration),
			formatDuration(step.Duration),
		)
	}

	return nil
}

func closeSandbox(item *sandbox.Sandbox) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if err := item.Close(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "close sandbox failed: %v\n", err)
		return
	}

	fmt.Printf("close: %s\n", item.Status)
}

func containsSandbox(items []*sandbox.Sandbox, runID int64, sshCommand string) bool {
	for _, item := range items {
		if item.RunID == runID && item.SSHCommand == sshCommand {
			return true
		}
	}
	return false
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

		if err := os.Setenv(strings.TrimSpace(key), strings.Trim(strings.TrimSpace(value), `"'`)); err != nil {
			return err
		}
	}

	return nil
}

func mustEnv(key string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		panic("missing env: " + key)
	}
	return value
}

func envOr(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func envDurationSeconds(key string, fallbackSeconds int) (time.Duration, error) {
	raw := envOr(key, fmt.Sprintf("%d", fallbackSeconds))
	value, err := time.ParseDuration(raw + "s")
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return value, nil
}

func formatDuration(value time.Duration) string {
	return fmt.Sprintf("%.2fs", value.Seconds())
}

func formatSpeed(bytes int64, duration time.Duration) string {
	if duration <= 0 {
		return "0.00 B/s"
	}

	perSecond := float64(bytes) / duration.Seconds()
	return formatUnit(perSecond, "/s")
}

func formatBytes(value int64) string {
	return formatUnit(float64(value), "")
}

func formatUnit(value float64, suffix string) string {
	sign := ""
	if value < 0 {
		sign = "-"
		value = -value
	}

	units := []string{"B", "KB", "MB", "GB"}
	unit := units[0]
	for i := 1; i < len(units) && value >= 1024; i++ {
		value /= 1024
		unit = units[i]
	}

	return fmt.Sprintf("%s%.2f %s%s", sign, value, unit, suffix)
}
