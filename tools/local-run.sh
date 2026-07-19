#!/bin/bash
set -e

godep go clean
godep go build

: "${PLATFORM_ACCESS_KEY:?Set PLATFORM_ACCESS_KEY for a local test account}"
: "${PLATFORM_SECRET_KEY:?Set PLATFORM_SECRET_KEY for a local test account}"
PLATFORM_URL=${PLATFORM_URL:-http://localhost:8080/v1} ./host-provisioner -loglevel=debug
