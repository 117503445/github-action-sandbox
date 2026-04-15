package sandbox_test

import (
	"context"
	"fmt"

	"github-action-sandbox/sdk/sandbox"
)

func ExampleCreateSandbox() {
	opts := sandbox.DefaultCreateSandboxOptions()
	opts.Name = "demo"
	opts.Image = "ubuntu:22.04"

	item, err := sandbox.CreateSandbox(context.Background(), opts)
	if err != nil {
		fmt.Println("create failed")
		return
	}

	fmt.Println(item.ID, item.Status)
}
