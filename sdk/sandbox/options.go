package sandbox

import "time"

// DefaultCreateSandboxOptions 返回创建 sandbox 的默认参数。
func DefaultCreateSandboxOptions() CreateSandboxOptions {
	return CreateSandboxOptions{
		GitHubWorkflow: "sandbox.yml",
		GitHubRef:      "main",
		UptermServer:   "ssh://uptermd.upterm.dev:22",
		StartupTimeout: 2 * time.Minute,
	}
}

// DefaultListSandboxesOptions 返回列出 sandbox 的默认参数。
func DefaultListSandboxesOptions() ListSandboxesOptions {
	return ListSandboxesOptions{
		GitHubWorkflow: "sandbox.yml",
		Limit:          20,
	}
}
