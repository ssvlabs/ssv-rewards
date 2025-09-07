package httpretry

import (
	"net/http"
	"time"

	"github.com/ybbus/httpretry"
)

// Clone default transport and only modify what we need
var transport = http.DefaultTransport.(*http.Transport).Clone()

func init() {
	// Reasonable timeout adjustments for SSV API connection issues
	
	// Increased TLS handshake timeout (default is 10s)
	transport.TLSHandshakeTimeout = 90 * time.Second
	
	// Connection pool settings - improved from defaults
	transport.MaxIdleConnsPerHost = 10     // Default is 2, increased for better reuse
}

var Client = httpretry.NewCustomClient(
	&http.Client{
		Transport: transport,
		// Overall request timeout (including retries)
		// Set high to allow for retries, individual request timeouts handled by transport
		Timeout: 10 * time.Minute,
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
