package conformance

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"time"
)

// Keep output draining independent of Wait: a descendant may inherit the pipe
// after the build parent exits. End the owned tree before waiting for its EOF.
func runBuildCommand(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	if cmd.Err != nil {
		return nil, cmd.Err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	defer writer.Close()
	type output struct {
		bytes []byte
		err   error
	}
	drained := make(chan output, 1)
	go func() {
		data, err := io.ReadAll(reader)
		drained <- output{data, err}
	}()
	err = runOwnedBuild(ctx, cmd, writer)
	_ = writer.Close()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case result := <-drained:
		return result.bytes, errors.Join(err, result.err)
	case <-timer.C:
		_ = reader.Close()
		result := <-drained
		return result.bytes, errors.Join(err, errors.New("build output remained open after process cleanup"))
	}
}
