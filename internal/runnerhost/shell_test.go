package runnerhost

import "testing"

func TestShellArgs(t *testing.T) {
	if got := shellArgs("zsh"); len(got) != 1 || got[0] != "-il" {
		t.Fatalf("unexpected zsh args: %#v", got)
	}
	if got := shellArgs("bash"); len(got) != 1 || got[0] != "-il" {
		t.Fatalf("unexpected bash args: %#v", got)
	}
	if got := shellArgs("sh"); len(got) != 1 || got[0] != "-i" {
		t.Fatalf("unexpected sh args: %#v", got)
	}
}
