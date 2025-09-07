package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ybbus/httpretry"
)

func main() {
	url := "https://api.ssv.network/api/v4/mainnet/validators/duty_counts/383400/383624"
	
	// Test with default httpretry settings
	fmt.Println("=== Test 1: httpretry with default settings ===")
	testDefault(url)
	
	// Test with your exact configuration
	fmt.Println("\n=== Test 2: httpretry with your config ===")
	testWithConfig(url)
}

func testDefault(url string) {
	start := time.Now()
	
	client := httpretry.NewDefaultClient()
	
	resp, err := client.Get(url)
	if err != nil {
		fmt.Printf("Error after %v: %v\n", time.Since(start), err)
		return
	}
	defer resp.Body.Close()
	
	fmt.Printf("Success after %v, Status: %s\n", time.Since(start), resp.Status)
	
	var data struct {
		Error      string
		Validators map[string]struct{ Duties int }
	}
	
	err = json.NewDecoder(resp.Body).Decode(&data)
	if err != nil {
		fmt.Printf("Error decoding: %v\n", err)
		return
	}
	
	fmt.Printf("Successfully decoded %d validators\n", len(data.Validators))
}

func testWithConfig(url string) {
	start := time.Now()
	
	// Match your exact configuration
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSHandshakeTimeout = 3 * time.Minute
	transport.ResponseHeaderTimeout = 2 * time.Minute
	transport.WriteBufferSize = 1 << 20
	transport.ReadBufferSize = 1 << 20
	transport.MaxIdleConnsPerHost = 2
	
	client := httpretry.NewCustomClient(
		&http.Client{
			Transport: transport,
			// No timeout like your config
		},
		httpretry.WithMaxRetryCount(10),
		httpretry.WithRetryPolicy(func(statusCode int, err error) bool {
			return err != nil || statusCode >= 500 || statusCode == 0 || statusCode == 429
		}),
		httpretry.WithBackoffPolicy(func(attemptNum int) time.Duration {
			return time.Duration(attemptNum+1) * 2 * time.Second
		}),
	)
	
	resp, err := client.Get(url)
	if err != nil {
		fmt.Printf("Error after %v: %v\n", time.Since(start), err)
		return
	}
	defer resp.Body.Close()
	
	fmt.Printf("Success after %v, Status: %s\n", time.Since(start), resp.Status)
	
	var data struct {
		Error      string
		Validators map[string]struct{ Duties int }
	}
	
	err = json.NewDecoder(resp.Body).Decode(&data)
	if err != nil {
		fmt.Printf("Error decoding: %v\n", err)
		return
	}
	
	fmt.Printf("Successfully decoded %d validators\n", len(data.Validators))
}