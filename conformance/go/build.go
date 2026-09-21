package conformance

import (
	"context"
	"time"
)

// withBuildDeadline gives one recipe's commands their own bounded window.
// Call it after preparation so rendering and earlier languages cannot spend
// this build's time. Both runtime and generated testees use the same bound.
func withBuildDeadline(build func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	return build(ctx)
}
