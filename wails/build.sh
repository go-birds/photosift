#!/usr/bin/env bash
# Build PhotoSift.app (macOS). Run this on a Mac with the Wails toolchain.
#
#   1. Install Go and the Wails CLI:
#        go install github.com/wailsapp/wails/v2/cmd/wails@latest
#   2. From this directory:
#        ./build.sh
#
# Output: build/bin/PhotoSift.app  (drag it to /Applications)
set -euo pipefail
cd "$(dirname "$0")"

if ! command -v wails >/dev/null 2>&1; then
  echo "wails CLI not found. Install it with:"
  echo "  go install github.com/wailsapp/wails/v2/cmd/wails@latest"
  exit 1
fi

# Universal binary so it runs on both Apple Silicon and Intel Macs.
wails build -platform darwin/universal -clean

echo
echo "Built build/bin/PhotoSift.app"
echo "It shares the database with the CLI at ~/.photosift/photosift.db,"
echo "so run 'photosift scan' and 'photosift analyze' first."
