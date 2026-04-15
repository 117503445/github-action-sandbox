package sandbox

// DefaultCreateSandboxOptions 返回创建沙箱的默认参数。
func DefaultCreateSandboxOptions() CreateSandboxOptions {
	return CreateSandboxOptions{
		Region: "us-east-1",
	}
}
