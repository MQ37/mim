#!/bin/sh
# Build the mim static binary.
set -e

CGO_ENABLED=0 go build -ldflags="-s -w" -o mim .

echo "built mim ($(du -h mim | cut -f1))"
