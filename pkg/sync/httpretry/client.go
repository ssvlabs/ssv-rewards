package httpretry

import (
	"time"

	"github.com/ybbus/httpretry"
)

var Client = httpretry.NewDefaultClient(
	httpretry.WithMaxRetryCount(10),

	// Retry on any error, 5xx status codes and 0 status codes.
	httpretry.WithRetryPolicy(func(statusCode int, err error) bool {
		return err != nil || statusCode >= 500 || statusCode == 0 || statusCode == 429
	}),

	// Retry with an incremental backoff policy.
	httpretry.WithBackoffPolicy(func(attemptNum int) time.Duration {
		return time.Duration(attemptNum+1) * 2 * time.Second
	}),
)
