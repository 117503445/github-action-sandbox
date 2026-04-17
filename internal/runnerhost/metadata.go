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

func metadataFromPublicURL(requestID string, sshUser string, publicURL string) (Metadata, error) {
	if strings.TrimSpace(requestID) == "" {
		return Metadata{}, errors.New("request id is required")
	}
	if strings.TrimSpace(sshUser) == "" {
		return Metadata{}, errors.New("ssh user is required")
	}
	if strings.TrimSpace(publicURL) == "" {
		return Metadata{}, errors.New("public url is required")
	}

	u, err := url.Parse(publicURL)
	if err != nil {
		return Metadata{}, err
	}
	if u.Scheme != "tcp" {
		return Metadata{}, fmt.Errorf("unsupported pinggy url scheme %q", u.Scheme)
	}

	host := u.Hostname()
	if host == "" {
		return Metadata{}, errors.New("missing pinggy host")
	}
	rawPort := u.Port()
	if rawPort == "" {
		return Metadata{}, errors.New("missing pinggy port")
	}

	port, err := net.LookupPort("tcp", rawPort)
	if err != nil {
		return Metadata{}, err
	}

	return Metadata{
		RequestID:  requestID,
		Status:     "running",
		SSHHost:    host,
		SSHPort:    port,
		SSHUser:    sshUser,
		SSHCommand: fmt.Sprintf("ssh -p %d %s@%s", port, sshUser, host),
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
