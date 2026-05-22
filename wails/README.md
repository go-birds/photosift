# PhotoSift desktop app

A clickable macOS app that renders the photosift review UI in its own window.
It shares the database with the CLI (`~/.photosift/photosift.db`), so the
workflow is:

```bash
photosift scan "/path/to/Takeout/Google Photos"
photosift analyze
# then open the app to review
```

There are two ways to build it.

## Option A — native window (Wails)

A real native-window app via [Wails v2](https://wails.io). This module reuses
`internal/server` directly: Wails accepts a custom `http.Handler` as its asset
server, so the same UI + `/api/*` routes that power `photosift serve` render
straight into the webview — no frontend duplication.

```bash
# one-time
go install github.com/wailsapp/wails/v2/cmd/wails@latest
# build (on a Mac)
./build.sh        # -> build/bin/PhotoSift.app
```

This is its own Go module (`go.mod` here) with a `replace` pointing at the
parent, so the heavy webview dependencies stay out of the main `photosift` CLI.

> Note: building the native app requires a Mac with the Wails toolchain
> (Xcode command-line tools, WebKit). The Go code compiles and links, but the
> `.app` itself must be produced on macOS.

## Option B — browser app, no toolchain

If you don't want to install Wails, this bundles the plain CLI binary into a
`.app` that launches `photosift serve` and opens your default browser. Pure Go,
no webview/CGO.

```bash
./build-app-from-cli.sh   # -> PhotoSift.app
open PhotoSift.app
```

## Configuration

Set `PHOTOSIFT_DB` to point either app at a different database file.
