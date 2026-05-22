// dhash.mjs — perceptual hashing helpers used by the (experimental) timeline
// matcher. The algorithm mirrors internal/ingest/metrics.go so hashes computed
// here are comparable to the ones photosift stored, letting us recognise a
// flagged photo from its Google Photos thumbnail.

// hammingDistance counts differing bits between two BigInt hashes.
export function hammingDistance(a, b) {
  let x = a ^ b;
  let count = 0;
  while (x > 0n) {
    count += Number(x & 1n);
    x >>= 1n;
  }
  return count;
}

// dHashFromRGBA computes the same 9x8 row-gradient hash photosift uses, given
// raw RGBA pixels and the image dimensions. Returns a BigInt.
export function dHashFromRGBA(pixels, width, height) {
  const W = 9,
    H = 8;
  const lum = [];
  for (let y = 0; y < H; y++) {
    lum[y] = [];
    for (let x = 0; x < W; x++) {
      const sx = Math.floor((x * width) / W);
      const sy = Math.floor((y * height) / H);
      const i = (sy * width + sx) * 4;
      const r = pixels[i] / 255;
      const g = pixels[i + 1] / 255;
      const b = pixels[i + 2] / 255;
      lum[y][x] = (0.299 * r + 0.587 * g + 0.114 * b) * 255;
    }
  }
  let out = 0n;
  let bit = 0n;
  for (let y = 0; y < H; y++) {
    for (let x = 0; x < W - 1; x++) {
      if (lum[y][x] > lum[y][x + 1]) out |= 1n << bit;
      bit++;
    }
  }
  return out;
}
