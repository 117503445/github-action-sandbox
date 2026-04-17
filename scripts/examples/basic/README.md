# Basic Example

最小端到端示例，只展示 4 件事：

1. 创建 sandbox
2. 打印公网 SSH 连接串
3. 调用 `ListSandboxes` 确认 run 可见
4. 调用 `FreeDiskSpace()`，最后关闭 sandbox

用法：

```bash
cp .env.example .env
go run .
```

必填环境变量：

- `GITHUB_TOKEN`
- `GITHUB_REPOSITORY`

可选环境变量：

- `GITHUB_WORKFLOW`，默认 `sandbox.yml`
- `GITHUB_REF`，默认 `master`
- `SANDBOX_NAME`，默认 `basic-example`
- `PINGGY_TOKEN`，为空时使用匿名隧道
- `STARTUP_TIMEOUT_SECONDS`，默认 `300`
- `CLEANUP_TIMEOUT_SECONDS`，默认 `900`
