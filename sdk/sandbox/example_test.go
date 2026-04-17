package sandbox_test

import (
	"fmt"

	"github-action-sandbox/sdk/sandbox"
)

func ExampleDefaultCreateSandboxOptions() {
	opts := sandbox.DefaultCreateSandboxOptions()

	fmt.Println(opts.GitHubWorkflow)
	fmt.Println(opts.GitHubRef)
	fmt.Println(opts.UptermServer)

	// Output:
	// sandbox.yml
	// main
	// ssh://uptermd.upterm.dev:22
}
