package client

import (
	"context"
	"time"
)

// sleep performs a delay, respecting context cancellation
func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-time.After(d):
	case <-ctx.Done():
	}
}
