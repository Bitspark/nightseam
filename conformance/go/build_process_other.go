//go:build !unix && !windows

package conformance

import (
	"context"
	"errors"
	"os"
	"os/exec"
)

func runOwnedBuild(context.Context, *exec.Cmd, *os.File) error {
	return errors.New("conformance builds require Unix process groups or Windows job objects")
}
