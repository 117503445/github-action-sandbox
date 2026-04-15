package sandbox

// Sandbox 表示创建后的通用沙箱对象。
type Sandbox struct {
	ID     string
	Status string
}

// CreateSandboxOptions 定义创建沙箱所需的输入参数。
type CreateSandboxOptions struct {
	Name   string
	Region string
	Image  string
}
