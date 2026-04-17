package sandbox

import "time"

// DefaultCreateSandboxOptions 返回创建 sandbox 的默认参数。
func DefaultCreateSandboxOptions() CreateSandboxOptions {
	return CreateSandboxOptions{
		GitHubWorkflow: "sandbox.yml",
		GitHubRef:      "main",
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

// DefaultFreeDiskSpaceOptions 返回基于 SSH 清理磁盘的默认参数。
func DefaultFreeDiskSpaceOptions() FreeDiskSpaceOptions {
	return FreeDiskSpaceOptions{
		Android:       true,
		Dotnet:        true,
		Haskell:       true,
		LargePackages: true,
		DockerImages:  true,
		ToolCache:     false,
		SwapStorage:   true,
	}
}
