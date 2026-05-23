#!/usr/bin/env python3
"""Semantic content classifier for photosift's "useless content" category.

Google Photos cleanup gets a lot more useful when the tool can recognise
*content*: photos of documents/receipts, screenshots, products on a store
shelf, or the broken part you photographed to match at the hardware store.
Those are semantic judgements, so this step uses a real model — zero-shot CLIP —
rather than pixel heuristics.

It runs out-of-process on purpose: the Go service stays fast and dependency
free, while this optional pass produces a labels.json that you import with
`photosift tag -i labels.json`.

Why zero-shot CLIP: we score each photo against a small set of natural-language
prompts ("a photo of a document", "a product on a store shelf", ...). No
training, no fixed label set — you can edit PROMPTS below to taste.

Setup (one time):
    python3 -m venv .venv && . .venv/bin/activate
    pip install open_clip_torch pillow torch

Usage:
    python3 classify.py /path/to/takeout/Google\\ Photos -o ../labels.json
    cd .. && ./photosift tag -i labels.json && ./photosift analyze
"""

import argparse
import json
import os
import sys

# Prompts grouped by whether the content is "useless" (a deletion candidate) or
# "keep" (a real photo). The label written back to photosift is the winning
# prompt's bucket name; the matcher in internal/analyze treats any "useless"
# bucket as category 4.
USELESS_PROMPTS = {
    "document": ["a scan of a document", "a photo of a printed page of text"],
    "receipt": ["a photo of a receipt", "a photo of a paper bill"],
    "screenshot": ["a screenshot of a phone screen", "a screenshot of a computer screen"],
    "product in a store": [
        "a photo of a product on a store shelf",
        "a price tag in a shop",
        "a product label or packaging",
    ],
    "broken item to replace": [
        "a close-up photo of a broken part",
        "a photo of a hardware part to match at the store",
    ],
    "whiteboard or notes": ["a photo of a whiteboard", "a photo of handwritten notes"],
}
KEEP_PROMPTS = {
    "photo": [
        "a photograph of a person",
        "a landscape photograph",
        "a photo of food",
        "a photo of an animal",
        "a snapshot of an event",
    ],
}

IMAGE_EXTS = {".jpg", ".jpeg", ".png", ".gif", ".webp", ".heic", ".heif"}


def iter_images(root):
    for dirpath, _, files in os.walk(root):
        for name in files:
            if os.path.splitext(name)[1].lower() in IMAGE_EXTS:
                yield os.path.join(dirpath, name)


def build_prompt_table():
    flat, buckets, useless = [], [], []
    for bucket, prompts in {**USELESS_PROMPTS, **KEEP_PROMPTS}.items():
        is_useless = bucket in USELESS_PROMPTS
        for p in prompts:
            flat.append(p)
            buckets.append(bucket)
            useless.append(is_useless)
    return flat, buckets, useless


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("root", help="Takeout 'Google Photos' folder (or any image dir)")
    ap.add_argument("-o", "--out", default="labels.json")
    ap.add_argument("--model", default="ViT-B-32")
    ap.add_argument("--pretrained", default="laion2b_s34b_b79k")
    ap.add_argument("--batch", type=int, default=32)
    args = ap.parse_args()

    try:
        import torch
        import open_clip
        from PIL import Image
    except ImportError:
        sys.exit("missing deps; run: pip install open_clip_torch pillow torch")

    device = "cuda" if torch.cuda.is_available() else "cpu"
    model, _, preprocess = open_clip.create_model_and_transforms(
        args.model, pretrained=args.pretrained
    )
    model = model.to(device).eval()
    tokenizer = open_clip.get_tokenizer(args.model)

    prompts, buckets, useless = build_prompt_table()
    with torch.no_grad():
        text = tokenizer(prompts).to(device)
        text_features = model.encode_text(text)
        text_features /= text_features.norm(dim=-1, keepdim=True)

    paths = list(iter_images(args.root))
    print(f"classifying {len(paths)} images on {device}...", file=sys.stderr)
    out = []

    for start in range(0, len(paths), args.batch):
        chunk = paths[start : start + args.batch]
        imgs, ok = [], []
        for p in chunk:
            try:
                imgs.append(preprocess(Image.open(p).convert("RGB")))
                ok.append(p)
            except Exception as e:  # noqa: BLE001
                print(f"skip {p}: {e}", file=sys.stderr)
        if not imgs:
            continue
        batch = torch.stack(imgs).to(device)
        with torch.no_grad():
            feats = model.encode_image(batch)
            feats /= feats.norm(dim=-1, keepdim=True)
            sims = (100.0 * feats @ text_features.T).softmax(dim=-1)
        for i, p in enumerate(ok):
            best = int(sims[i].argmax())
            out.append(
                {
                    "path": os.path.abspath(p),
                    "label": buckets[best] if useless[best] else "photo",
                    "score": float(sims[i][best]),
                }
            )
        print(f"  {start + len(ok)}/{len(paths)}", file=sys.stderr)

    with open(args.out, "w") as f:
        json.dump(out, f, indent=2)
    print(f"wrote {len(out)} labels to {args.out}", file=sys.stderr)


if __name__ == "__main__":
    main()
