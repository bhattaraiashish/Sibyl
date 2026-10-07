#!/usr/bin/env bash

set -e

echo "Building Sibyl..."
go build -o sibyl ./cmd/sibyl

echo "Building Sibyl Dashboard..."
go build -o sibyl-dashboard ./cmd/sibyl-dashboard

echo "Starting Sibyl..."
./sibyl &

echo "Starting Sibyl Dashboard..."
./sibyl-dashboard &

wait
