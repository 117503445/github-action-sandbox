package runnerhost

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestRunWritesMetadataFromFakeUpterm(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	fakeBin := t.TempDir()
	fakeUptermPath := filepath.Join(fakeBin, "upterm")
	fakeScript := `#!/bin/sh
set -eu
cmd="$1"
shift
case "$cmd" in
  host)
    echo "Session: fake-session"
    echo
    echo "Command:          bash -il"
    echo "Host:             ssh://uptermd.upterm.dev:22"
    trap 'exit 0' TERM INT
    while :; do sleep 1; done
    ;;
  session)
    sub="$1"
    shift
    if [ "$sub" != "current" ]; then
      exit 1
    fi
    echo '{"sessionId":"fake-session","host":"ssh://uptermd.upterm.dev:22","command":"bash -il","forceCommand":""}'
    ;;
  *)
    exit 1
    ;;
esac
`
	if err := os.WriteFile(fakeUptermPath, []byte(fakeScript), 0o755); err != nil {
		t.Fatalf("write fake upterm: %v", err)
	}

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+origPath)

	ctx, cancel := context.WithCancel(context.Background())
	ctx = zerolog.Nop().WithContext(ctx)
	defer cancel()

	metadataPath := filepath.Join(t.TempDir(), "metadata.json")
	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- Run(ctx, Options{
			RequestID:      "req-123",
			UptermServer:   "ssh://uptermd.upterm.dev:22",
			MetadataPath:   metadataPath,
			StartupTimeout: 2 * time.Second,
		})
	}()

	deadline := time.Now().Add(3 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for metadata")
		}
		data, err := os.ReadFile(metadataPath)
		if err == nil {
			content := string(data)
			if !strings.Contains(content, `"ssh_user": "fake-session"`) {
				t.Fatalf("unexpected metadata content: %s", content)
			}
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	cancel()

	select {
	case err := <-runErrCh:
		if err == nil {
			t.Fatal("expected context cancellation error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for Run to finish")
	}
}
