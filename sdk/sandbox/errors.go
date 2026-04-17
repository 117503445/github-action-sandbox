package sandbox

import "errors"

var (
	ErrInvalidOptions       = errors.New("sandbox: invalid options")
	ErrWorkflowDispatch     = errors.New("sandbox: workflow dispatch failed")
	ErrWorkflowStartTimeout = errors.New("sandbox: workflow start timeout")
	ErrMetadataTimeout      = errors.New("sandbox: metadata not published")
	ErrSandboxFailed        = errors.New("sandbox: workflow failed before sandbox was ready")
)
