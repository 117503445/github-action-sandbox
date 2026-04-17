package runnerhost

import "testing"

func TestMetadataFromPublicURL(t *testing.T) {
	metadata, err := metadataFromPublicURL("req-1", "root", "tcp://demo.a.pinggy.link:43000")
	if err != nil {
		t.Fatalf("metadataFromPublicURL returned error: %v", err)
	}

	if metadata.RequestID != "req-1" {
		t.Fatalf("unexpected request id: %q", metadata.RequestID)
	}
	if metadata.Status != "running" {
		t.Fatalf("unexpected status: %q", metadata.Status)
	}
	if metadata.SSHHost != "demo.a.pinggy.link" || metadata.SSHPort != 43000 {
		t.Fatalf("unexpected ssh endpoint: %+v", metadata)
	}
	if metadata.SSHUser != "root" {
		t.Fatalf("unexpected ssh user: %q", metadata.SSHUser)
	}
	if metadata.SSHCommand != "ssh -p 43000 root@demo.a.pinggy.link" {
		t.Fatalf("unexpected ssh command: %q", metadata.SSHCommand)
	}
}

func TestMetadataFromPublicURLRejectsUnsupportedScheme(t *testing.T) {
	_, err := metadataFromPublicURL("req-1", "root", "https://demo.a.pinggy.link:43000")
	if err == nil {
		t.Fatal("expected error for unsupported scheme")
	}
}
