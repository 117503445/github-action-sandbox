package runnerhost

import "testing"

func TestMetadataFromSession(t *testing.T) {
	metadata, err := metadataFromSession("req-1", currentSession{
		SessionID: "session-token",
		Host:      "ssh://uptermd.upterm.dev:22",
		Command:   "/bin/bash -il",
	})
	if err != nil {
		t.Fatalf("metadataFromSession returned error: %v", err)
	}

	if metadata.RequestID != "req-1" {
		t.Fatalf("unexpected request id: %q", metadata.RequestID)
	}
	if metadata.Status != "running" {
		t.Fatalf("unexpected status: %q", metadata.Status)
	}
	if metadata.SSHHost != "uptermd.upterm.dev" || metadata.SSHPort != 22 {
		t.Fatalf("unexpected ssh endpoint: %+v", metadata)
	}
	if metadata.SSHUser != "session-token" {
		t.Fatalf("unexpected ssh user: %q", metadata.SSHUser)
	}
	if metadata.SSHCommand != "ssh -p 22 session-token@uptermd.upterm.dev" {
		t.Fatalf("unexpected ssh command: %q", metadata.SSHCommand)
	}
}

func TestMetadataFromSessionRejectsUnsupportedScheme(t *testing.T) {
	_, err := metadataFromSession("req-1", currentSession{
		SessionID: "session-token",
		Host:      "wss://uptermd.example.com",
	})
	if err == nil {
		t.Fatal("expected error for unsupported scheme")
	}
}
