#
# Build stage using Debian for glibc
#
FROM golang:1.22-bookworm AS build
WORKDIR /app

# Copy the go.mod and go.sum first and download the dependencies. 
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,mode=0755,target=/go/pkg \
    go mod download

# Copy the rest of the source code
COPY . .

# Build the binary with caching
# CGO is enabled for potential C dependencies
ENV CGO_ENABLED=1
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,mode=0755,target=/go/pkg \
    go build -o /bin/ssv-rewards ./cmd/ssv-rewards

#
# Run stage.
#
FROM debian:bookworm-slim
WORKDIR /app

# Install ca-certificates for HTTPS
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

# Copy the built binary from the previous stage
COPY --from=build /bin/ssv-rewards /bin/ssv-rewards

ENTRYPOINT ["/bin/ssv-rewards"]