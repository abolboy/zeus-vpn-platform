package wireguard

import (
	"context"
	"errors"
	"os/exec"
)

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, binary string, args ...string) error {
	command := exec.CommandContext(ctx, binary, args...)
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin"}
	if output, err := command.CombinedOutput(); err != nil {
		_ = output
		return errors.New("WireGuard command failed")
	}
	return nil
}
