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

	PinggyToken string

	// Deprecated: UptermServer is ignored. Use PinggyToken.
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

// FreeDiskSpaceOptions 定义基于 SSH 在 sandbox 内执行磁盘清理时的选项。
type FreeDiskSpaceOptions struct {
	Android       bool
	Dotnet        bool
	Haskell       bool
	LargePackages bool
	DockerImages  bool
	ToolCache     bool
	SwapStorage   bool
}

// DiskCleanupStep 表示一次清理步骤的执行结果。
type DiskCleanupStep struct {
	Name string

	StartedAt   time.Time
	CompletedAt time.Time
	Duration    time.Duration

	AvailableBytesBefore int64
	AvailableBytesAfter  int64
	FreedBytes           int64

	Command string
}

// FreeDiskSpaceResult 汇总一次磁盘清理的结果。
type FreeDiskSpaceResult struct {
	StartedAt   time.Time
	CompletedAt time.Time
	Duration    time.Duration

	AvailableBytesBefore int64
	AvailableBytesAfter  int64
	FreedBytes           int64

	Steps []DiskCleanupStep
}
