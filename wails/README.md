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

## Releasing a signed, notarized .app

The release workflow (`.github/workflows/release.yml`) signs and notarizes the
universal `.app` automatically when these GitHub Action secrets exist on the
repo. Without them you still get a working unsigned build that users have to
right-click → Open the first time.

| secret               | what it is                                                                 |
|----------------------|-----------------------------------------------------------------------------|
| `APPLE_CERT_P12`     | Your "Developer ID Application" cert exported as `.p12`, then base64-encoded. |
| `APPLE_CERT_PASSWORD`| The password you set when exporting the `.p12`.                            |
| `APPLE_DEV_ID`       | The cert's common name, e.g. `Developer ID Application: Jane Doe (TEAMID)`. |
| `APPLE_ID`           | Your Apple Developer account email.                                        |
| `APPLE_APP_PASSWORD` | An [app-specific password](https://appleid.apple.com) for `notarytool`.    |
| `APPLE_TEAM_ID`      | Your 10-character team ID.                                                 |

One-time setup on your Mac to produce the `.p12`:

```bash
# In Keychain Access, find your "Developer ID Application" cert, right-click,
# Export -> Personal Information Exchange (.p12), choose a password.
# Then encode it for the secret:
base64 -i DeveloperID.p12 | pbcopy
# Paste into the APPLE_CERT_P12 secret on GitHub.
```

You need a paid Apple Developer membership ($99/yr) for the cert.
