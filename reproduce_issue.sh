#!/bin/bash

echo "==================================================================="
echo "Reproducing Go HTTP Client + Docker Bridge Network Issue"
echo "==================================================================="

# Create a test Go program
cat > test_ssv.go << 'EOF'
package main

import (
    "fmt"
    "io"
    "net/http"
    "os"
    "time"
)

func main() {
    url := "https://api.ssv.network/api/v4/mainnet/clusters"
    
    fmt.Printf("Testing SSV API endpoint...\n")
    fmt.Printf("URL: %s\n", url)
    fmt.Printf("Starting at: %s\n\n", time.Now().Format("15:04:05"))
    
    client := &http.Client{
        Timeout: 2 * time.Minute,
    }
    
    start := time.Now()
    resp, err := client.Get(url)
    if err != nil {
        fmt.Printf("❌ FAILED after %v\n", time.Since(start))
        fmt.Printf("Error: %v\n", err)
        os.Exit(1)
    }
    defer resp.Body.Close()
    
    body, err := io.ReadAll(resp.Body)
    if err != nil {
        fmt.Printf("❌ Failed to read body: %v\n", err)
        os.Exit(1)
    }
    
    fmt.Printf("✅ SUCCESS after %v\n", time.Since(start))
    fmt.Printf("Response: %d bytes\n", len(body))
}
EOF

# Create a Dockerfile for the test
cat > Dockerfile.test << 'EOF'
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY test_ssv.go .
RUN go build -o test_ssv test_ssv.go

FROM alpine:3.18
RUN apk add --no-cache ca-certificates curl
COPY --from=builder /app/test_ssv /test_ssv
CMD ["/test_ssv"]
EOF

echo "Building test container..."
docker build -f Dockerfile.test -t ssv-test . > /dev/null 2>&1

echo -e "\n==================================================================="
echo "TEST 1: Curl works fine in Docker container"
echo "==================================================================="
docker run --rm ssv-test sh -c "curl -s -o /dev/null -w 'Curl result: HTTP %{http_code}, Size: %{size_download} bytes, Time: %{time_total}s\n' https://api.ssv.network/api/v4/mainnet/clusters"

echo -e "\n==================================================================="
echo "TEST 2: Go HTTP client with Docker BRIDGE network (THIS WILL FAIL)"
echo "==================================================================="
timeout 120 docker run --rm ssv-test /test_ssv

echo -e "\n==================================================================="
echo "TEST 3: Go HTTP client with HOST network (THIS WILL WORK)"
echo "==================================================================="
docker run --rm --network host ssv-test /test_ssv

echo -e "\n==================================================================="
echo "TEST 4: Go binary directly on host (THIS WILL WORK)"
echo "==================================================================="
go run test_ssv.go

echo -e "\n==================================================================="
echo "SUMMARY:"
echo "- Curl works everywhere"
echo "- Go HTTP client FAILS with Docker bridge network"
echo "- Go HTTP client WORKS with host network"
echo "- Go HTTP client WORKS directly on host"
echo "This is why we need network_mode: host as a workaround"
echo "==================================================================="

# Cleanup
rm -f test_ssv.go Dockerfile.test
docker rmi ssv-test > /dev/null 2>&1