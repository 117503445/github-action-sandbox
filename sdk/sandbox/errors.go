package sandbox

import "errors"

var (
	// ErrNotImplemented 表示当前能力尚未实现。
	ErrNotImplemented = errors.New("sandbox: not implemented")
)
