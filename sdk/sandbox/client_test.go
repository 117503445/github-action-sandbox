package sandbox

import (
	"context"
	"testing"
)

func TestCreateSandboxSignature(t *testing.T) {
	_, err := CreateSandbox(context.Background(), DefaultCreateSandboxOptions())
	if err == nil {
		t.Fatal("expected not implemented error")
	}
}
