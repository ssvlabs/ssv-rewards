#!/bin/bash

echo "Testing SSV API endpoints from Docker container with bridge network..."
echo "============================================================="

# Test 1: Simple endpoint (should be fast)
echo -e "\n1. Testing /clusters endpoint (small response):"
time docker run --rm alpine/curl:latest -s -o /dev/null -w "Status: %{http_code}, Size: %{size_download} bytes, Time: %{time_total}s\n" \
  --connect-timeout 10 --max-time 30 \
  https://api.ssv.network/api/v4/mainnet/clusters

# Test 2: Another simple endpoint
echo -e "\n2. Testing /operators endpoint (medium response):"
time docker run --rm alpine/curl:latest -s -o /dev/null -w "Status: %{http_code}, Size: %{size_download} bytes, Time: %{time_total}s\n" \
  --connect-timeout 10 --max-time 30 \
  https://api.ssv.network/api/v4/mainnet/operators

# Test 3: The problematic endpoint
echo -e "\n3. Testing /validators/duty_counts endpoint (14MB response):"
time docker run --rm alpine/curl:latest -s -o /dev/null -w "Status: %{http_code}, Size: %{size_download} bytes, Time: %{time_total}s\n" \
  --max-time 120 \
  https://api.ssv.network/api/v4/mainnet/validators/duty_counts/383400/383624

echo -e "\n============================================================="
echo "Testing with host network mode for comparison..."
echo "============================================================="

# Test with host network
echo -e "\n4. Testing /clusters with host network:"
time docker run --rm --network host alpine/curl:latest -s -o /dev/null -w "Status: %{http_code}, Size: %{size_download} bytes, Time: %{time_total}s\n" \
  https://api.ssv.network/api/v4/mainnet/clusters

echo -e "\n5. Testing problematic endpoint with host network:"
time docker run --rm --network host alpine/curl:latest -s -o /dev/null -w "Status: %{http_code}, Size: %{size_download} bytes, Time: %{time_total}s\n" \
  --max-time 120 \
  https://api.ssv.network/api/v4/mainnet/validators/duty_counts/383400/383624