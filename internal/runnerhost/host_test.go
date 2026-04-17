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

func TestRunWritesMetadataFromFakeSSHDevAndPinggy(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	fakeBin := t.TempDir()
	fakeSSHDevPath := filepath.Join(fakeBin, "sshdev")
	fakeSSHPath := filepath.Join(fakeBin, "ssh")

	fakeSSHDevScript := `#!/bin/sh
set -eu
exec python3 -m http.server 2222 --bind 127.0.0.1 >/dev/null 2>&1
`
	if err := os.WriteFile(fakeSSHDevPath, []byte(fakeSSHDevScript), 0o755); err != nil {
		t.Fatalf("write fake sshdev: %v", err)
	}

	fakeSSHScript := `#!/bin/sh
set -eu
last=""
for arg in "$@"; do
  last="$arg"
done
if [ "$last" != "tcp@a.pinggy.io" ]; then
  echo "unexpected target: $last" >&2
  exit 1
fi
echo "tcp://demo.a.pinggy.link:43000"
trap 'exit 0' TERM INT
while :; do sleep 1; done
`
	if err := os.WriteFile(fakeSSHPath, []byte(fakeSSHScript), 0o755); err != nil {
		t.Fatalf("write fake ssh: %v", err)
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
			MetadataPath:   metadataPath,
			StartupTimeout: 4 * time.Second,
		})
	}()

	expectedUser := currentSSHUser()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for metadata")
		}
		data, err := os.ReadFile(metadataPath)
		if err == nil {
			content := string(data)
			if !strings.Contains(content, `"ssh_host": "demo.a.pinggy.link"`) {
				t.Fatalf("unexpected metadata content: %s", content)
			}
			if !strings.Contains(content, `"ssh_command": "ssh -p 43000 `+expectedUser+`@demo.a.pinggy.link"`) {
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
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Run to finish")
	}
}
