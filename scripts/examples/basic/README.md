# Basic Example

该目录是独立的 Go module。

在当前目录准备好 `.env`，或者先导出所需环境变量后直接运行：

```bash
go run .
```

示例会执行完整链路：

1. 调用 `CreateSandbox`
2. 使用返回的 SSH 信息进入 runner
3. 调用 `ListSandboxes`，确认刚创建的 sandbox 能被列出且 SSH 信息一致
4. 执行探针命令，确认进入 `root` shell 且工作目录位于 runner workspace
5. 调用 `Close()`
