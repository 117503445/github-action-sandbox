# Basic Example

该目录是独立的 Go module。为了在仓库内验证当前工作区代码，`go.mod` 通过本地 `replace` 指向仓库根目录。

在当前目录准备好 `.env`，或者先导出所需环境变量后直接运行：

```bash
go run .
```

常用环境变量：

- `STARTUP_TIMEOUT_SECONDS`：等待 workflow 发布 metadata 的超时，默认 `300`
- `CLEANUP_TIMEOUT_SECONDS`：等待 `FreeDiskSpace()` 完成的超时，默认 `900`

示例会执行完整链路：

1. 调用 `CreateSandbox`
2. 输出返回的 SSH 连接信息
3. 调用 `ListSandboxes`，确认刚创建的 sandbox 能被列出且 SSH 信息一致
4. 调用 `FreeDiskSpace()`，通过 SSH 执行清理，并输出每个清理项的开始/结束时间、耗时和释放空间
5. 调用 `Close()`
