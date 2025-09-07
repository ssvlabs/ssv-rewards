package httpretry

import (
	"net/http"
	"time"

	"github.com/ybbus/httpretry"
)

// Clone default transport and only modify what we need
var transport = http.DefaultTransport.(*http.Transport).Clone()

func init() {
	// Adjustments for SSV API's large responses (14MB+ JSON)
	
	// Increased TLS handshake timeout for large response processing
	transport.TLSHandshakeTimeout = 3 * time.Minute
	
	// Large response buffer settings for 14MB+ responses
	transport.WriteBufferSize = 1 << 20         // 1MB write buffer (default 4KB)
	transport.ReadBufferSize = 1 << 20          // 1MB read buffer (default 4KB)
	
	// Connection pool settings - keep minimal to avoid connection issues
	transport.MaxIdleConnsPerHost = 2     // Reduced to avoid stale connections
}

var Client = httpretry.NewCustomClient(
	&http.Client{
		Transport: transport,
		// No client-level timeout to allow large responses (14MB+) to complete
		// Transport-level timeouts will handle connection issues
	},
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
