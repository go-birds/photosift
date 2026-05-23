# photosift

Local-first tool that finds photos worth deleting and gives you a desktop
review UI to triage them. Built for cleaning up a Google Photos library exported
via Google Takeout.

## What it suggests

photosift scores every photo and groups deletion candidates into five
categories:

1. **Exact duplicates** — byte-identical files (SHA-256).
2. **Near duplicates** — visually identical frames, e.g. burst shots
   (perceptual dHash + Hamming clustering). Keeps the sharpest/highest-res.
3. **Low quality** — blurry/soft focus (Laplacian variance), very dark, or
   blown-out images.
4. **Useless content** — documents, receipts, screenshots, products on a store
   shelf, the broken part you photographed for the hardware store. Pixel
   heuristics catch documents/screenshots out of the box; semantic cases use an
   optional ML pass (see `ml/`).
5. **Too many of one subject** — large clusters of similar photos; keeps the
   best few and flags the surplus.

Nothing is ever deleted automatically. You review in the UI, mark what to
remove, and either export the list or run the opt-in deleter.

## A note on Google Photos and deletion

**Google Photos has no API for deleting library photos** (it never has), and
since March 2025 Google removed broad library read access too. So photosift
analyses a **Google Takeout export** on your disk, and the actual deletion
happens either by your hand in the Google Photos app or via the opt-in browser
automation in `automation/` (which drives the web UI in your own logged-in
browser; deleted items sit in Google Photos Trash for 60 days).

## Workflow

```bash
# 1. Export your library from https://takeout.google.com (Google Photos), unzip.

# 2. Build
go build -o photosift ./cmd/photosift

# 3. Index the export (fast: one decode per file, batched writes, skips
#    unchanged files on re-runs)
./photosift scan "/path/to/Takeout/Google Photos"

# 4. (optional) Semantic labels for 'useless content' — see ml/classify.py
python3 ml/classify.py "/path/to/Takeout/Google Photos" -o labels.json
./photosift tag -i labels.json

# 5. Score and produce suggestions
./photosift analyze

# 6. Review in the desktop UI (opens a local web app)
./photosift serve            # then open http://127.0.0.1:8765

# 7. Either export the delete list...
./photosift export -o photosift-delete.json
#    ...or run the opt-in deleter (dry run by default)
cd automation && npm install && node delete.mjs --manifest ../photosift-delete.json
```

## Commands

| command   | purpose                                                        |
|-----------|----------------------------------------------------------------|
| `scan`    | Index image files; computes hashes + quality metrics.          |
| `analyze` | Score images and write deletion suggestions.                   |
| `tag`     | Import semantic content labels from the ML pass.               |
| `ml`      | Go-native ML labelling (opt-in build, MobileNetV2 via ONNX).   |
| `serve`   | Local review UI (the desktop app).                             |
| `export`  | Write the delete manifest (confirmed deletions, or all).       |
| `dupes`   | Quick CLI listing of exact duplicates.                         |

Useful flags: `analyze --near-dist`, `--subject-min`, `--blur-threshold`,
`--overrides`, `--no-learn`; `serve --addr`; all commands take `--db`.

## Per-album threshold overrides

A nature album shot at golden hour has different priors than a folder of
photos-of-shopping-receipts. Pass `--overrides overrides.json` to
`photosift analyze`:

```json
[
  {"prefix": "/photos/Italy 2024",  "blur_threshold": 80},
  {"prefix": "/photos/screenshots", "useless_threshold": 0.3, "blur_threshold": 30}
]
```

The longest matching prefix wins, so narrow rules can nest inside broad ones.
Unset fields inherit the global thresholds. Clustering radii are global.

## Learning from your decisions

After you review and mark some suggestions, the next `photosift analyze`
notices which categories you've been overriding and tightens those thresholds.
The trigger is conservative — at least 20 decisions in a category, and over
half marked "keep" — so a few clicks won't move things. Pass `--no-learn` to
disable.

## ML pipelines

Category 4 ("useless content") has two ML paths and you can use either:

- **`ml/classify.py`** (default — works out of the box). Zero-shot CLIP via
  Python with prompt-engineered buckets. Higher accuracy on store/product
  scenes; requires `pip install open_clip_torch pillow torch`. Writes a JSON
  imported with `photosift tag`.
- **`photosift ml`** (Go-native, opt-in build). MobileNetV2 ImageNet classifier
  via [onnxruntime_go](https://github.com/yalue/onnxruntime_go) dlopen'd at
  run time. Less semantic coverage (1000 fixed classes vs free-form prompts)
  but no Python dependency and one less moving part.

  ```bash
  # one-time: install the ONNX runtime shared library and point at it
  #   macOS:  brew install onnxruntime
  #   linux:  apt install libonnxruntime-dev   (or build from source)
  go build -tags onnx -o photosift ./cmd/photosift
  PHOTOSIFT_ONNX_LIB=/usr/local/lib/libonnxruntime.so \
    ./photosift ml          # auto-downloads model + labels on first run
  ./photosift analyze       # now uses the ML labels for category 4
  ```

## A note on HEIC

iPhone's default `.heic`/`.heif` format has no pure-Go decoder, so photosift
shells out to whichever converter is installed: `heif-convert` from
[libheif](https://github.com/strukturag/libheif) (`brew install libheif`,
`apt install libheif-examples`) or the `sips` tool that ships with macOS.
If neither is installed, HEIC files are counted as "could not be decoded" and
photosift prints a one-shot install hint.

## Performance

- One decode per file; dHash, blur, brightness and colourfulness are computed
  from a single downscaled buffer.
- Writes are batched into transactions through a single SQLite writer
  (WAL, `synchronous=NORMAL`).
- Re-scans skip files whose size + mtime are unchanged.
- Near-duplicate clustering uses a BK-tree over perceptual hashes, so it stays
  fast on large libraries instead of comparing every pair.
- Workers scale to `NumCPU`.

## Mac app

A clickable `PhotoSift.app` lives in [`wails/`](wails/). It renders this same UI
in a native window and shares the CLI database (`~/.photosift/photosift.db`).
Two build paths are provided:

- **Native window (Wails):** `cd wails && ./build.sh` — reuses `internal/server`
  as the Wails asset handler, so there's no duplicated frontend. Requires a Mac
  with the Wails toolchain.
- **No toolchain:** `cd wails && ./build-app-from-cli.sh` — bundles the plain Go
  binary into a `.app` that runs `serve` and opens your browser.

See [`wails/README.md`](wails/README.md) for details. The web assets are
embedded in the binary (`go:embed`), so there are no loose files to ship.
