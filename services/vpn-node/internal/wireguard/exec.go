package wireguard

import (
	"context"
	"errors"
	"os/exec"
)

type ExecRunner struct{}

func commandEnvironment(command *exec.Cmd) {
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin"}
}

func (ExecRunner) Run(ctx context.Context, binary string, args ...string) error {
	command := exec.CommandContext(ctx, binary, args...)
	commandEnvironment(command)
	if output, err := command.CombinedOutput(); err != nil {
		_ = output
		return errors.New("system networking command failed")
	}
	return nil
}

func (ExecRunner) Output(ctx context.Context, binary string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, binary, args...)
	commandEnvironment(command)
	output, err := command.Output()
	if err != nil {
		return nil, errors.New("system route inspection command failed")
	}
	return output, nil
}
