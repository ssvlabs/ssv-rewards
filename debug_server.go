package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"time"
)

func main() {
	url := "https://api.ssv.network/api/v4/mainnet/validators/duty_counts/383400/383624"
	
	// Test 1: Basic http.Client with no timeout
	fmt.Println("=== Test 1: Basic http.Client (no timeout) ===")
	testBasicClient(url)
	
	// Test 2: http.Client with very long timeout  
	fmt.Println("\n=== Test 2: http.Client with 5 min timeout ===")
	testWithTimeout(url)
	
	// Test 3: With httptrace to see what's happening
	fmt.Println("\n=== Test 3: With httptrace diagnostics ===")
	testWithTrace(url)
}

func testBasicClient(url string) {
	start := time.Now()
	client := &http.Client{}
	
	resp, err := client.Get(url)
	if err != nil {
		fmt.Printf("Error after %v: %v\n", time.Since(start), err)
		return
	}
	defer resp.Body.Close()
	
	fmt.Printf("Success after %v, Status: %s\n", time.Since(start), resp.Status)
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("Body size: %d bytes\n", len(body))
}

func testWithTimeout(url string) {
	start := time.Now()
	client := &http.Client{
		Timeout: 5 * time.Minute,
	}
	
	resp, err := client.Get(url)
	if err != nil {
		fmt.Printf("Error after %v: %v\n", time.Since(start), err)
		return
	}
	defer resp.Body.Close()
	
	fmt.Printf("Success after %v, Status: %s\n", time.Since(start), resp.Status)
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("Body size: %d bytes\n", len(body))
}

func testWithTrace(url string) {
	start := time.Now()
	
	trace := &httptrace.ClientTrace{
		DNSStart: func(dsi httptrace.DNSStartInfo) {
			fmt.Printf("[%v] DNS lookup starting for %s\n", time.Since(start), dsi.Host)
		},
		DNSDone: func(ddi httptrace.DNSDoneInfo) {
			fmt.Printf("[%v] DNS lookup done: %v\n", time.Since(start), ddi.Addrs)
		},
		ConnectStart: func(network, addr string) {
			fmt.Printf("[%v] Connecting to %s %s\n", time.Since(start), network, addr)
		},
		ConnectDone: func(network, addr string, err error) {
			fmt.Printf("[%v] Connected to %s %s (err: %v)\n", time.Since(start), network, addr, err)
		},
		TLSHandshakeStart: func() {
			fmt.Printf("[%v] TLS handshake starting\n", time.Since(start))
		},
		TLSHandshakeDone: func(cs tls.ConnectionState, err error) {
			fmt.Printf("[%v] TLS handshake done (err: %v)\n", time.Since(start), err)
		},
		GotFirstResponseByte: func() {
			fmt.Printf("[%v] Got first response byte!\n", time.Since(start))
		},
	}
	
	req, _ := http.NewRequest("GET", url, nil)
	req = req.WithContext(httptrace.WithClientTrace(context.Background(), trace))
	
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("[%v] Error: %v\n", time.Since(start), err)
		return
	}
	defer resp.Body.Close()
	
	fmt.Printf("[%v] Got response headers, Status: %s\n", time.Since(start), resp.Status)
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("[%v] Body size: %d bytes\n", time.Since(start), len(body))
}