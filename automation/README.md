# photosift Google Photos deleter (opt-in)

Google Photos has no delete API, so this script automates deletion by driving
the photos.google.com web UI in a real browser.

**Read this before running:**

- **Dry run by default.** Add `--apply` to actually delete.
- **It uses your own browser profile.** A persistent `--user-data-dir` keeps you
  logged in; you sign in to Google by hand the first time. The script never
  handles your password.
- **It deletes via each photo's direct URL** (the `gphotos_url` from the
  manifest, which comes from Takeout sidecars). Photos without a URL are written
  to `manual-review.json` for you to delete by hand — the script does not guess.
- **Deletions are recoverable** from Google Photos Trash for 60 days.
- **Caveat:** automating your Google account is a grey area under Google's Terms
  of Service, and the web UI changes often, so the delete selectors in
  `delete.mjs` may need updating. Start with `--limit` a handful and a dry run.

## Usage

```bash
npm install

# Dry run — shows what would be deleted, deletes nothing
node delete.mjs --manifest ../photosift-delete.json

# Real deletion, conservatively
node delete.mjs --manifest ../photosift-delete.json --apply --limit 20 --delay 1500
```

| flag              | default              | meaning                              |
|-------------------|----------------------|--------------------------------------|
| `--manifest`      | (required)           | JSON from `photosift export`.        |
| `--apply`         | off                  | Actually delete (otherwise dry run). |
| `--user-data-dir` | `./.gphotos-profile` | Persistent browser profile.          |
| `--limit`         | all                  | Max photos to process this run.      |
| `--delay`         | `1200`               | Milliseconds between actions.        |

`dhash.mjs` mirrors photosift's perceptual hash and is provided for an
experimental timeline-matching approach (recognising flagged photos by their
thumbnail when no direct URL exists). It is not wired into the destructive path.
