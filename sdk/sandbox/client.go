package sandbox

import "context"

// CreateSandbox 创建一个新的沙箱实例。
//
// 当前版本仅固定 API 签名以保证后续兼容，具体逻辑将在后续迭代中实现。
func CreateSandbox(ctx context.Context, opts CreateSandboxOptions) (*Sandbox, error) {
	_ = ctx
	_ = opts
	return nil, ErrNotImplemented
}
