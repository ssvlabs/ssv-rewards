#!/bin/bash
# Build the test binary for Alpine Linux (musl)
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o test_alpine test_httpretry_server.go

# Copy it into a running container or create a test container
docker run --rm -v $(pwd)/test_alpine:/test_alpine alpine:3.18 /test_alpine
