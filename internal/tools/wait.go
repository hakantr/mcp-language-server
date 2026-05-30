package tools

import (
	"context"
	"fmt"
	"time"
)

func waitForServerProcessing(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait canceled: %w", ctx.Err())
	}
}
