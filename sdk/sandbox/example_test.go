package sandbox_test

import (
	"fmt"

	"github.com/117503445/github-action-sandbox/sdk/sandbox"
)

func ExampleDefaultCreateSandboxOptions() {
	opts := sandbox.DefaultCreateSandboxOptions()

	fmt.Println(opts.GitHubWorkflow)
	fmt.Println(opts.GitHubRef)
	fmt.Printf("%q\n", opts.PinggyToken)

	// Output:
	// sandbox.yml
	// main
	// ""
}

func ExampleDefaultListSandboxesOptions() {
	opts := sandbox.DefaultListSandboxesOptions()

	fmt.Println(opts.GitHubWorkflow)
	fmt.Println(opts.Limit)

	// Output:
	// sandbox.yml
	// 20
}

func ExampleDefaultFreeDiskSpaceOptions() {
	opts := sandbox.DefaultFreeDiskSpaceOptions()

	fmt.Println(opts.Android)
	fmt.Println(opts.ToolCache)

	// Output:
	// true
	// false
}
