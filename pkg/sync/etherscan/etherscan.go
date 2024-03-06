package etherscan

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/bloxapp/ssv-rewards/pkg/sync/httpretry"
	"github.com/carlmjohnson/requests"
	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/time/rate"
)

type Client struct {
	endpoint    string
	apiKey      string
	rateLimiter *rate.Limiter
}

func New(endpoint, apiKey string, requestsPerSecond float64) *Client {
	return &Client{
		endpoint: endpoint,
		apiKey:   apiKey,
		rateLimiter: rate.NewLimiter(
			rate.Every(time.Duration(float64(time.Second)/requestsPerSecond)),
			1,
		),
	}
}

type ContractCreation struct {
	ContractAddress []byte
	ContractCreator []byte
	TxHash          []byte
}

func (c *Client) ContractCreation(
	ctx context.Context,
	addresses []common.Address,
) ([]ContractCreation, error) {
	if len(addresses) == 0 {
		return nil, fmt.Errorf("no addresses")
	}
	addressStrings := make([]string, len(addresses))
	for i, address := range addresses {
		addressStrings[i] = address.String()
	}

	// Fetch from the Etherscan API.
	if err := c.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("failed to wait for rate limiter: %w", err)
	}
	var resp struct {
		Status  string
		Message string
		Result  json.RawMessage
	}
	err := requests.URL(c.endpoint).
		Client(httpretry.Client).
		Path("/api").
		Param("module", "contract").
		Param("action", "getcontractcreation").
		Param("contractaddresses", strings.Join(addressStrings, ",")).
		Param("apikey", c.apiKey).
		ToJSON(&resp).
		Fetch(ctx)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	if resp.Status != "1" || !strings.HasPrefix(resp.Message, "OK") {
		return nil, fmt.Errorf(
			"bad status %s (%s): %s",
			resp.Status,
			resp.Message,
			resp.Result,
		)
	}

	// Decode the response.
	var results []struct {
		ContractAddress string `json:"contractAddress"`
		ContractCreator string `json:"contractCreator"`
		TxHash          string `json:"txHash"`
	}
	if err := json.Unmarshal(resp.Result, &results); err != nil {
		return nil, fmt.Errorf("failed to unmarshal contract creation: %w", err)
	}
	if len(results) != len(addresses) {
		return nil, fmt.Errorf(
			"failed to get contract creation: expected %d results, got %d",
			len(addresses),
			len(results),
		)
	}
	creations := make([]ContractCreation, len(results))
	for i, result := range results {
		contractAddress, err := hex.DecodeString(result.ContractAddress[2:])
		if err != nil {
			return nil, fmt.Errorf("failed to decode deployer address: %w", err)
		}
		deployerAddress, err := hex.DecodeString(result.ContractCreator[2:])
		if err != nil {
			return nil, fmt.Errorf("failed to decode contract creator: %w", err)
		}
		txHash, err := hex.DecodeString(result.TxHash[2:])
		if err != nil {
			return nil, fmt.Errorf("failed to decode tx hash: %w", err)
		}
		if len(contractAddress) != 20 || len(deployerAddress) != 20 || len(txHash) != 32 {
			return nil, fmt.Errorf("failed to decode contract creation")
		}
		if !bytes.Equal(contractAddress, addresses[i].Bytes()) {
			return nil, fmt.Errorf("contract creation address mismatch")
		}
		creations[i] = ContractCreation{
			ContractAddress: contractAddress,
			ContractCreator: deployerAddress,
			TxHash:          txHash,
		}
	}
	return creations, nil
}
