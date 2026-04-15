# github-action-sandbox

一个用于管理 Sandbox 的 Go SDK 最小工程骨架。

## 快速开始

### 1) 安装依赖

```bash
go mod tidy
```

### 2) 运行测试

```bash
go test ./...
```

## 最小调用示例

```go
package main

import (
	"context"
	"fmt"

	"github-action-sandbox/sdk/sandbox"
)

func main() {
	opts := sandbox.DefaultCreateSandboxOptions()
	opts.Name = "demo"
	opts.Image = "ubuntu:22.04"

	item, err := sandbox.CreateSandbox(context.Background(), opts)
	if err != nil {
		fmt.Println("create sandbox failed:", err)
		return
	}

	fmt.Println("sandbox:", item.ID, item.Status)
}
```

> 当前 `CreateSandbox` 为占位实现，会返回 `sandbox: not implemented`。API 签名已固定，便于后续向后兼容迭代。
