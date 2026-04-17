# github-action-sandbox

一个用于创建、列出和关闭 GitHub Actions sandbox 的 Go SDK。

## 快速开始

### 1) 安装依赖

```bash
go mod tidy
```

### 2) 运行测试

```bash
go test ./...
cd scripts/examples/basic && go test ./...
```

## 最小调用示例

```go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/117503445/github-action-sandbox/sdk/sandbox"
)

func main() {
	opts := sandbox.DefaultCreateSandboxOptions()
	opts.Name = "demo"
	opts.GitHubRepository = "owner/repo"
	opts.GitHubToken = os.Getenv("GITHUB_TOKEN")
	opts.PinggyToken = os.Getenv("PINGGY_TOKEN") // 可选；为空时使用匿名隧道

	item, err := sandbox.CreateSandbox(context.Background(), opts)
	if err != nil {
		fmt.Println("create sandbox failed:", err)
		return
	}

	items, err := sandbox.ListSandboxes(context.Background(), sandbox.ListSandboxesOptions{
		GitHubRepository: opts.GitHubRepository,
		GitHubToken:      opts.GitHubToken,
	})
	if err != nil {
		fmt.Println("list sandboxes failed:", err)
		return
	}

	fmt.Println("sandboxes:", len(items))
	fmt.Println("ssh:", item.SSHCommand)

	cleanup := sandbox.DefaultFreeDiskSpaceOptions()
	cleanup.ToolCache = true

	report, err := item.FreeDiskSpace(context.Background(), cleanup)
	if err != nil {
		fmt.Println("free disk space failed:", err)
		return
	}

	for _, step := range report.Steps {
		fmt.Printf("%s: freed=%dB duration=%s\n", step.Name, step.FreedBytes, step.Duration)
	}

	if err := item.Close(context.Background()); err != nil {
		fmt.Println("close sandbox failed:", err)
	}
}
```

`scripts/examples/basic` 是独立的 Go module，可以直接在该目录执行 `go run .`。

创建完成后，`Sandbox.SSHCommand` 会返回一个标准 SSH 连接串，形如 `ssh -p <port> <user>@<host>`。SDK 在 runner 上启动 `sshdev`，再通过 `pinggy.io` 暴露到公网；未提供 `PINGGY_TOKEN` 时使用匿名隧道。

## 磁盘清理

`Sandbox.FreeDiskSpace` 会通过 SSH 连接到已创建的 sandbox，并参考 `jlumbroso/free-disk-space@main` 的清理项顺序执行：

- `android`
- `dotnet`
- `haskell`
- `large-packages`
- `docker-images`
- `tool-cache`
- `swap-storage`

返回结果会包含每个清理项的开始时间、结束时间、耗时，以及清理前后可用空间和实际释放字节数。默认选项与该 Action 保持一致，其中 `tool-cache` 默认关闭，其余默认开启。
