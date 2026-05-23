#!/usr/bin/env bash
# Fallback: build PhotoSift.app WITHOUT the Wails toolchain.
#
# This bundles the plain `photosift` CLI binary into a clickable .app whose only
# job is to run `photosift serve` and open the review UI in your default
# browser. No webview/CGO/Wails dependencies — just Go. Use this if you don't
# want to install the Wails CLI; use build.sh for the true native-window app.
#
#   ./build-app-from-cli.sh
#   open PhotoSift.app
set -euo pipefail
cd "$(dirname "$0")"

APP="PhotoSift.app"
ADDR="127.0.0.1:8765"

rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"

echo "building photosift binary..."
(cd .. && go build -o "wails/$APP/Contents/MacOS/photosift" ./cmd/photosift)

cat > "$APP/Contents/MacOS/PhotoSift" <<EOF
#!/bin/bash
DIR="\$(cd "\$(dirname "\$0")" && pwd)"
ADDR="$ADDR"
( sleep 1; open "http://\$ADDR" ) &
exec "\$DIR/photosift" serve --addr "\$ADDR"
EOF
chmod +x "$APP/Contents/MacOS/PhotoSift"

cat > "$APP/Contents/Info.plist" <<'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>PhotoSift</string>
  <key>CFBundleDisplayName</key><string>PhotoSift</string>
  <key>CFBundleIdentifier</key><string>com.gobirds.photosift</string>
  <key>CFBundleVersion</key><string>0.1.0</string>
  <key>CFBundleExecutable</key><string>PhotoSift</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>LSMinimumSystemVersion</key><string>10.13</string>
  <key>LSUIElement</key><true/>
</dict>
</plist>
EOF

echo "built $APP — open it with: open $APP"
echo "(shares ~/.photosift/photosift.db with the CLI; scan + analyze first)"
