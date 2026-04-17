package runnerhost

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Metadata is the structured artifact published back to the SDK.
type Metadata struct {
	RequestID  string `json:"request_id"`
	Status     string `json:"status"`
	SSHHost    string `json:"ssh_host"`
	SSHPort    int    `json:"ssh_port"`
	SSHUser    string `json:"ssh_user"`
	SSHCommand string `json:"ssh_command"`
}

type currentSession struct {
	SessionID string `json:"sessionId"`
	Host      string `json:"host"`
	Command   string `json:"command"`
}

func metadataFromSession(requestID string, session currentSession) (Metadata, error) {
	if strings.TrimSpace(requestID) == "" {
		return Metadata{}, errors.New("request id is required")
	}
	if strings.TrimSpace(session.SessionID) == "" {
		return Metadata{}, errors.New("session id is required")
	}
	if strings.TrimSpace(session.Host) == "" {
		return Metadata{}, errors.New("host is required")
	}

	u, err := url.Parse(session.Host)
	if err != nil {
		return Metadata{}, err
	}
	if u.Scheme != "" && u.Scheme != "ssh" {
		return Metadata{}, fmt.Errorf("unsupported upterm server scheme %q", u.Scheme)
	}

	host := u.Hostname()
	if host == "" {
		return Metadata{}, errors.New("missing upterm host")
	}

	port := 22
	if rawPort := u.Port(); rawPort != "" {
		parsed, err := net.LookupPort("tcp", rawPort)
		if err != nil {
			return Metadata{}, err
		}
		port = parsed
	}

	return Metadata{
		RequestID:  requestID,
		Status:     "running",
		SSHHost:    host,
		SSHPort:    port,
		SSHUser:    session.SessionID,
		SSHCommand: fmt.Sprintf("ssh -p %d %s@%s", port, session.SessionID, host),
	}, nil
}

func writeMetadata(path string, metadata Metadata) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	payload, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')

	return os.WriteFile(path, payload, 0o644)
}
