package sandbox

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestDefaultFreeDiskSpaceOptions(t *testing.T) {
	opts := DefaultFreeDiskSpaceOptions()

	if !opts.Android {
		t.Fatalf("expected android cleanup enabled by default")
	}
	if !opts.Dotnet {
		t.Fatalf("expected dotnet cleanup enabled by default")
	}
	if !opts.Haskell {
		t.Fatalf("expected haskell cleanup enabled by default")
	}
	if !opts.LargePackages {
		t.Fatalf("expected large packages cleanup enabled by default")
	}
	if !opts.DockerImages {
		t.Fatalf("expected docker image cleanup enabled by default")
	}
	if opts.ToolCache {
		t.Fatalf("expected tool cache cleanup disabled by default")
	}
	if !opts.SwapStorage {
		t.Fatalf("expected swap cleanup enabled by default")
	}
}

func TestFreeDiskSpaceRequiresSSHAccess(t *testing.T) {
	_, err := (&Sandbox{}).FreeDiskSpace(context.Background(), DefaultFreeDiskSpaceOptions())
	if !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("expected ErrInvalidOptions, got %v", err)
	}
}

func TestFreeDiskSpaceReportsPerStepMeasurements(t *testing.T) {
	originalRunner := sshScriptRunner
	defer func() {
		sshScriptRunner = originalRunner
	}()

	executedSteps := make([]string, 0, 2)

	sshScriptRunner = func(ctx context.Context, s *Sandbox, script string) (sshCommandOutput, error) {
		switch {
		case strings.Contains(script, availableBytesBeforeMarker) && strings.Contains(script, "/usr/local/lib/android"):
			executedSteps = append(executedSteps, "android")
			return sshCommandOutput{
				Stdout: fmt.Sprintf("%s %d\n%s %d\n", availableBytesBeforeMarker, 1000, availableBytesAfterMarker, 1500),
			}, nil
		case strings.Contains(script, availableBytesBeforeMarker) && strings.Contains(script, "AGENT_TOOLSDIRECTORY"):
			executedSteps = append(executedSteps, "tool-cache")
			return sshCommandOutput{
				Stdout: fmt.Sprintf("%s %d\n%s %d\n", availableBytesBeforeMarker, 1500, availableBytesAfterMarker, 2100),
			}, nil
		default:
			t.Fatalf("unexpected script: %q", script)
			return sshCommandOutput{}, nil
		}
	}

	sbx := &Sandbox{
		ID:      "demo",
		SSHHost: "uptermd.upterm.dev",
		SSHPort: 22,
		SSHUser: "session-token",
	}

	result, err := sbx.FreeDiskSpace(context.Background(), FreeDiskSpaceOptions{
		Android:   true,
		ToolCache: true,
	})
	if err != nil {
		t.Fatalf("FreeDiskSpace returned error: %v", err)
	}

	if len(result.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(result.Steps))
	}
	if result.AvailableBytesBefore != 1000 {
		t.Fatalf("unexpected initial available bytes: %d", result.AvailableBytesBefore)
	}
	if result.AvailableBytesAfter != 2100 {
		t.Fatalf("unexpected final available bytes: %d", result.AvailableBytesAfter)
	}
	if result.FreedBytes != 1100 {
		t.Fatalf("unexpected total freed bytes: %d", result.FreedBytes)
	}

	first := result.Steps[0]
	if first.Name != "android" {
		t.Fatalf("unexpected first step name: %q", first.Name)
	}
	if first.AvailableBytesBefore != 1000 || first.AvailableBytesAfter != 1500 {
		t.Fatalf("unexpected first step bytes: %+v", first)
	}
	if first.FreedBytes != 500 {
		t.Fatalf("unexpected first step freed bytes: %d", first.FreedBytes)
	}
	if first.StartedAt.IsZero() || first.CompletedAt.IsZero() {
		t.Fatalf("expected first step timestamps to be set: %+v", first)
	}
	if !strings.Contains(first.Command, "/usr/local/lib/android") {
		t.Fatalf("expected first step command to remove android tools, got %q", first.Command)
	}

	second := result.Steps[1]
	if second.Name != "tool-cache" {
		t.Fatalf("unexpected second step name: %q", second.Name)
	}
	if second.AvailableBytesBefore != 1500 || second.AvailableBytesAfter != 2100 {
		t.Fatalf("unexpected second step bytes: %+v", second)
	}
	if second.FreedBytes != 600 {
		t.Fatalf("unexpected second step freed bytes: %d", second.FreedBytes)
	}
	if !strings.Contains(second.Command, "AGENT_TOOLSDIRECTORY") {
		t.Fatalf("expected second step to reference AGENT_TOOLSDIRECTORY, got %q", second.Command)
	}

	if strings.Join(executedSteps, ",") != "android,tool-cache" {
		t.Fatalf("unexpected executed steps: %v", executedSteps)
	}
	if result.StartedAt.IsZero() || result.CompletedAt.IsZero() {
		t.Fatalf("expected overall timestamps to be set: %+v", result)
	}
}

func TestParseInt64FromOutput(t *testing.T) {
	value, err := parseInt64FromOutput("warning\n12345\n")
	if err != nil {
		t.Fatalf("parseInt64FromOutput returned error: %v", err)
	}
	if value != 12345 {
		t.Fatalf("unexpected parsed value: %d", value)
	}
}

func TestParseInt64FromOutputWithPromptNoise(t *testing.T) {
	value, err := parseInt64FromOutput("\x1b[1;32m</work/project#\x1b[0m " + availableBytesMarker + " 4096 </work/project# 1\r\n\x1b[?2004l</work/project# ")
	if err != nil {
		t.Fatalf("parseInt64FromOutput returned error: %v", err)
	}
	if value != 4096 {
		t.Fatalf("unexpected parsed value: %d", value)
	}
}
