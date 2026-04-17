package sandbox_test

import (
	"fmt"

	"github.com/117503445/github-action-sandbox/sdk/sandbox"
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

func ExampleDefaultListSandboxesOptions() {
	opts := sandbox.DefaultListSandboxesOptions()

	fmt.Println(opts.GitHubWorkflow)
	fmt.Println(opts.Limit)

	// Output:
	// sandbox.yml
	// 20
}
