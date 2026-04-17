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

	if err := item.Close(context.Background()); err != nil {
		fmt.Println("close sandbox failed:", err)
	}
}
```

`scripts/examples/basic` 是独立的 Go module，可以直接在该目录执行 `go run .`。
