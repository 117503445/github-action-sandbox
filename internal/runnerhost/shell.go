package runnerhost

import (
	"errors"
	"os/exec"
	"path/filepath"
)

// Shell describes the interactive shell hosted inside upterm.
type Shell struct {
	Path string
	Args []string
}

// Command returns the argv used after the `--` separator in `upterm host`.
func (s Shell) Command() []string {
	args := []string{s.Path}
	args = append(args, s.Args...)
	return args
}

// SelectShell chooses the first available shell in zsh -> bash -> sh order.
func SelectShell() (Shell, error) {
	candidates := []string{"zsh", "bash", "sh"}
	for _, name := range candidates {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		return Shell{
			Path: path,
			Args: shellArgs(filepath.Base(path)),
		}, nil
	}

	return Shell{}, errors.New("no usable shell found")
}

func shellArgs(name string) []string {
	switch name {
	case "zsh", "bash":
		return []string{"-il"}
	default:
		return []string{"-i"}
	}
}
