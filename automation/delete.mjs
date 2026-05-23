// delete.mjs — opt-in Google Photos deleter.
//
// Google provides NO API for deleting library photos, so the only way to
// automate cleanup is to drive the photos.google.com web UI in a real browser.
// This script does that. It is deliberately conservative:
//
//   * Dry-run by default. It only deletes when you pass --apply.
//   * It uses YOUR browser profile (a persistent user-data dir) so you log in
//     to Google once, by hand. The script never sees your password.
//   * It deletes via each photo's direct Google Photos URL (the "url" field in
//     the manifest, populated from Takeout sidecars). Entries without a URL are
//     written to manual-review.json instead of guessed at.
//   * Deleted items go to Google Photos Trash and are recoverable for 60 days.
//
// IMPORTANT: automating your Google account is a grey area under Google's Terms
// of Service and the web UI changes often, so selectors may need updating. Run
// a dry run first and start with --limit a few.
//
// Usage:
//   npm install
//   node delete.mjs --manifest ../photosift-delete.json            # dry run
//   node delete.mjs --manifest ../photosift-delete.json --apply     # delete
//   node delete.mjs --manifest ... --apply --limit 10 --delay 1500

import { chromium } from "playwright";
import { readFile, writeFile } from "node:fs/promises";
import { matchEntry } from "./timeline-match.mjs";

function parseArgs(argv) {
  const args = {
    manifest: null,
    apply: false,
    userDataDir: "./.gphotos-profile",
    limit: Infinity,
    delay: 1200,
    matchTimeline: false,
  };
  for (let i = 2; i < argv.length; i++) {
    const a = argv[i];
    if (a === "--apply") args.apply = true;
    else if (a === "--match-timeline") args.matchTimeline = true;
    else if (a === "--manifest") args.manifest = argv[++i];
    else if (a === "--user-data-dir") args.userDataDir = argv[++i];
    else if (a === "--limit") args.limit = parseInt(argv[++i], 10);
    else if (a === "--delay") args.delay = parseInt(argv[++i], 10);
    else throw new Error(`unknown argument: ${a}`);
  }
  if (!args.manifest) throw new Error("--manifest <path> is required");
  return args;
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// clickDelete finds and clicks the trash control on an open photo page, then
// confirms. Google localises and reshuffles these controls, so we try several
// known accessible names. Returns true if a delete was triggered.
async function clickDelete(page, apply) {
  const trashNames = [/^Delete$/i, /Move to trash/i, /Trash/i];
  let trashBtn = null;
  for (const name of trashNames) {
    const btn = page.getByRole("button", { name });
    if (await btn.count()) {
      trashBtn = btn.first();
      break;
    }
  }
  if (!trashBtn) {
    // Keyboard shortcut: Shift+# deletes the open photo in Google Photos.
    if (!apply) return "would-delete (via shortcut)";
    await page.keyboard.press("Shift+#");
  } else {
    if (!apply) return "would-delete (via button)";
    await trashBtn.click();
  }

  // Confirm dialog, when present.
  await sleep(400);
  for (const name of [/Move to trash/i, /Delete/i, /Move/i]) {
    const confirm = page.getByRole("button", { name });
    if (await confirm.count()) {
      await confirm.first().click();
      break;
    }
  }
  return "deleted";
}

async function main() {
  const args = parseArgs(process.argv);
  const manifest = JSON.parse(await readFile(args.manifest, "utf8"));

  const withUrl = manifest.filter((m) => m.gphotos_url);
  const withoutUrl = manifest.filter((m) => !m.gphotos_url);

  console.log(`manifest: ${manifest.length} photos`);
  console.log(`  with Google Photos URL:    ${withUrl.length}`);
  if (args.matchTimeline) {
    console.log(`  without URL (will match):  ${withoutUrl.length}`);
  } else {
    console.log(`  without URL (will skip):   ${withoutUrl.length}`);
  }
  console.log(args.apply ? "MODE: APPLY (will delete)" : "MODE: DRY RUN (no deletions)");
  if (args.matchTimeline) {
    console.log(
      "MATCHER: EXPERIMENTAL — date-search + perceptual hashing, untested against live Google Photos",
    );
  }

  if (withoutUrl.length && !args.matchTimeline) {
    await writeFile("manual-review.json", JSON.stringify(withoutUrl, null, 2));
    console.log(
      `  -> wrote manual-review.json; pass --match-timeline to attempt matching, or delete these by hand`,
    );
  }

  const ctx = await chromium.launchPersistentContext(args.userDataDir, {
    headless: false,
    viewport: { width: 1280, height: 900 },
  });
  const page = ctx.pages()[0] || (await ctx.newPage());

  // Make sure we're logged in.
  await page.goto("https://photos.google.com/", { waitUntil: "domcontentloaded" });
  if (page.url().includes("accounts.google.com")) {
    console.log("\nPlease sign in to Google in the opened window, then press Enter here...");
    await new Promise((r) => process.stdin.once("data", r));
  }

  let done = 0;
  const results = { deleted: 0, dryRun: 0, failed: 0, unmatched: 0 };

  // Pass 1: direct URL deletes — fast and unambiguous.
  for (const item of withUrl) {
    if (done >= args.limit) break;
    done++;
    try {
      await page.goto(item.gphotos_url, { waitUntil: "domcontentloaded" });
      await sleep(args.delay);
      const outcome = await clickDelete(page, args.apply);
      if (outcome === "deleted") results.deleted++;
      else results.dryRun++;
      console.log(`[${done}] ${outcome}: ${item.path}`);
    } catch (err) {
      results.failed++;
      console.log(`[${done}] FAILED: ${item.path} (${err.message})`);
    }
    await sleep(args.delay);
  }

  // Pass 2: perceptual-hash timeline matching for entries without a URL.
  // Opt-in (--match-timeline) and conservative: skips on any ambiguity.
  if (args.matchTimeline) {
    for (const item of withoutUrl) {
      if (done >= args.limit) break;
      done++;
      try {
        const match = await matchEntry(page, item, {
          log: (m) => console.log(m),
        });
        if (!match) {
          results.unmatched++;
          console.log(`[${done}] unmatched: ${item.path}`);
          continue;
        }
        if (!args.apply) {
          results.dryRun++;
          console.log(
            `[${done}] would-delete (timeline match, dist=${match.distance}): ${item.path}`,
          );
        } else {
          // Click the matched tile, then run the same delete flow.
          await page.mouse.click(
            match.rect.x + match.rect.w / 2,
            match.rect.y + match.rect.h / 2,
          );
          await sleep(args.delay);
          const outcome = await clickDelete(page, true);
          if (outcome === "deleted") {
            results.deleted++;
            console.log(`[${done}] deleted (dist=${match.distance}): ${item.path}`);
          } else {
            results.failed++;
            console.log(`[${done}] match found but delete failed: ${item.path}`);
          }
        }
      } catch (err) {
        results.failed++;
        console.log(`[${done}] FAILED: ${item.path} (${err.message})`);
      }
      await sleep(args.delay);
    }
  }

  console.log(
    `\nDone. deleted=${results.deleted} dryRun=${results.dryRun} unmatched=${results.unmatched} failed=${results.failed}`,
  );
  if (results.deleted) console.log("Deleted photos are in Google Photos Trash for 60 days.");
  await ctx.close();
  process.exit(0);
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
