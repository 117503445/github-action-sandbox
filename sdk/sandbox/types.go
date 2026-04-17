package sandbox

import (
	"time"

	"github.com/117503445/github-action-sandbox/internal/githubactions"
)

// Sandbox 表示一个已创建的 GitHub Actions sandbox。
type Sandbox struct {
	ID     string
	Status string

	Repository string
	Workflow   string
	Ref        string
	RunID      int64
	RunURL     string

	SSHHost    string
	SSHPort    int
	SSHUser    string
	SSHCommand string

	CreatedAt time.Time

	client *githubactions.Client
}

// CreateSandboxOptions 定义创建 sandbox 所需的输入参数。
type CreateSandboxOptions struct {
	Name string

	GitHubRepository string
	GitHubWorkflow   string
	GitHubRef        string
	GitHubToken      string

	UptermServer string

	StartupTimeout time.Duration
}

// ListSandboxesOptions 定义列出 sandbox 所需的输入参数。
type ListSandboxesOptions struct {
	GitHubRepository string
	GitHubWorkflow   string
	GitHubToken      string

	Limit int
}
