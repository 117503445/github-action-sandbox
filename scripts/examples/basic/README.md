# Basic Example

该目录是独立的 Go module。

在当前目录准备好 `.env` 后直接运行：

```bash
go run .
```

示例会执行完整链路：

1. 调用 `CreateSandbox`
2. 使用返回的 SSH 信息进入 runner
3. 执行探针命令，确认进入 `root` shell 且工作目录位于 runner workspace
4. 调用 `Close()`
