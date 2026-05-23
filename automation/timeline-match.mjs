// timeline-match.mjs — EXPERIMENTAL perceptual-hash matcher for photos that
// lack a direct gphotos_url in the manifest.
//
// What this does: search Google Photos for photos taken on the same calendar
// day as the manifest entry, capture each result thumbnail's pixel data inside
// the browser, compute a dHash compatible with photosift's, and pick the
// closest match within a strict Hamming threshold. If exactly one strong match
// exists, return it; otherwise return null (we never guess).
//
// What this is not: tested. Google Photos' DOM changes often, search-by-date
// behaviour is undocumented, and a wrong delete is permanent within 60 days.
// The deleter only invokes this when you pass --match-timeline, and even then
// honours --apply / dry-run.

import { hammingDistance, dHashFromRGBA } from "./dhash.mjs";

const MAX_HAMMING = 4; // strict — Hamming 4/64 is "almost certainly the same image"
const SECOND_BEST_MARGIN = 6; // best must beat the second-best by this much

// formatDateQuery turns a Unix timestamp into the date string Google Photos'
// search bar accepts (e.g. "2024-03-15").
function formatDateQuery(unix) {
  const d = new Date(unix * 1000);
  const yyyy = d.getUTCFullYear();
  const mm = String(d.getUTCMonth() + 1).padStart(2, "0");
  const dd = String(d.getUTCDate()).padStart(2, "0");
  return `${yyyy}-${mm}-${dd}`;
}

// collectThumbnails grabs pixel data for every visible image tile on the
// current page. We sample at 9x8 (the dHash grid) directly via canvas to keep
// the cross-process payload tiny.
async function collectThumbnails(page) {
  return await page.evaluate(() => {
    // Google Photos uses obfuscated class names but every image tile is an
    // <img> whose src starts with the lh3.googleusercontent.com CDN. That's a
    // far more durable selector than any class name.
    const imgs = Array.from(document.querySelectorAll("img"))
      .filter((img) => /googleusercontent\.com/.test(img.src))
      .filter((img) => img.naturalWidth >= 64 && img.naturalHeight >= 64);

    const out = [];
    for (const img of imgs) {
      try {
        const canvas = document.createElement("canvas");
        canvas.width = 9;
        canvas.height = 8;
        const ctx = canvas.getContext("2d", { willReadFrequently: true });
        ctx.drawImage(img, 0, 0, 9, 8);
        const data = ctx.getImageData(0, 0, 9, 8).data;
        out.push({
          src: img.src,
          // Array.from converts Uint8ClampedArray for JSON transport.
          pixels: Array.from(data),
          // Bounding box lets us click the tile later.
          rect: (() => {
            const r = img.getBoundingClientRect();
            return { x: r.x, y: r.y, w: r.width, h: r.height };
          })(),
        });
      } catch {
        // Cross-origin tainting; skip.
      }
    }
    return out;
  });
}

// matchEntry runs one search and returns a single, unambiguous match — or null.
export async function matchEntry(page, entry, opts = {}) {
  const log = opts.log || (() => {});

  if (!entry.taken_at) {
    log("  no taken_at; cannot date-search");
    return null;
  }

  const query = formatDateQuery(entry.taken_at);
  await page.goto(
    `https://photos.google.com/search/${encodeURIComponent(query)}`,
    { waitUntil: "domcontentloaded" },
  );
  // Let lazy-loaded thumbnails settle.
  await new Promise((r) => setTimeout(r, opts.settleMs || 1500));

  const candidates = await collectThumbnails(page);
  if (candidates.length === 0) {
    log(`  no thumbnails returned for ${query}`);
    return null;
  }

  const target = BigInt(entry.dhash);
  let best = null;
  let bestDist = 999;
  let secondBestDist = 999;
  for (const c of candidates) {
    const h = dHashFromRGBA(c.pixels, 9, 8);
    const d = hammingDistance(target, h);
    if (d < bestDist) {
      secondBestDist = bestDist;
      bestDist = d;
      best = c;
    } else if (d < secondBestDist) {
      secondBestDist = d;
    }
  }

  if (bestDist > MAX_HAMMING) {
    log(`  no thumbnail within ${MAX_HAMMING} bits (closest: ${bestDist})`);
    return null;
  }
  if (secondBestDist - bestDist < SECOND_BEST_MARGIN) {
    log(
      `  ambiguous: best=${bestDist} secondBest=${secondBestDist} (need ${SECOND_BEST_MARGIN} gap)`,
    );
    return null;
  }
  return { src: best.src, rect: best.rect, distance: bestDist };
}
