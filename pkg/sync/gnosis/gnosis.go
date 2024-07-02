package gnosis

import (
	"context"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/bloxapp/ssv-rewards/pkg/sync/httpretry"
	"github.com/carlmjohnson/requests"
	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/time/rate"
)

var ErrNotFound = fmt.Errorf("not found")

type Client struct {
	endpoint    string
	rateLimiter *rate.Limiter
}

func New(endpoint string, requestsPerSecond float64) *Client {
	return &Client{
		endpoint: endpoint,
		rateLimiter: rate.NewLimiter(
			rate.Every(time.Duration(float64(time.Second)/requestsPerSecond)),
			1,
		),
	}
}

type Safe struct {
	Address   common.Address
	Threshold int
	Version   string
}

func (c *Client) Safe(
	ctx context.Context,
	address common.Address,
) (*Safe, error) {
	if err := c.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("failed to wait for rate limiter: %w", err)
	}
	var resp struct {
		Address   string
		Threshold int
		Version   string
	}

	err := requests.URL(c.endpoint).
		Client(httpretry.Client).
		Pathf("/api/v1/safes/%s/", address.String()).
		CheckStatus(200).
		ToJSON(&resp).
		Fetch(ctx)
	if requests.HasStatusErr(err, 404) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	if resp.Address != address.String() {
		return nil, fmt.Errorf("address mismatch")
	}

	// Decode the response.
	if len(resp.Address) != 42 {
		return nil, fmt.Errorf("invalid address")
	}
	if resp.Threshold < 1 {
		return nil, fmt.Errorf("invalid threshold")
	}
	if resp.Version == "" {
		return nil, fmt.Errorf("invalid version")
	}
	respAddress, err := hex.DecodeString(resp.Address[2:])
	if err != nil {
		return nil, fmt.Errorf("failed to parse address: %w", err)
	}
	return &Safe{
		Address:   common.BytesToAddress(respAddress),
		Threshold: resp.Threshold,
		Version:   resp.Version,
	}, nil
}
