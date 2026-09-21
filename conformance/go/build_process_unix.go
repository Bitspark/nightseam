//go:build unix

package conformance

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func runOwnedBuild(_ context.Context, cmd *exec.Cmd, output *os.File) error {
	cmd.Stdout, cmd.Stderr = output, output
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stop := func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.Cancel = stop
	if err := cmd.Start(); err != nil {
		return err
	}
	err := cmd.Wait()
	// Success also retires descendants: no build work owns a lifetime beyond
	// its command, even if the parent exited before its child did.
	cleanup := stop()
	if errors.Is(cleanup, os.ErrProcessDone) {
		cleanup = nil
	}
	return errors.Join(err, cleanup)
}
