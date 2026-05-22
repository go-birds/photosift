package sim

import (
	"math/rand"
	"testing"
)

// TestBKTreeMatchesBruteForce verifies the tree returns exactly the same set of
// neighbours as an O(n) scan, which is the property clustering relies on.
func TestBKTreeMatchesBruteForce(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	const n = 2000
	hashes := make([]uint64, n)
	for i := range hashes {
		hashes[i] = rng.Uint64()
	}

	var tree BKTree
	for i, h := range hashes {
		tree.Add(h, int64(i))
	}
	if tree.Len() != n {
		t.Fatalf("tree len = %d, want %d", tree.Len(), n)
	}

	for _, radius := range []int{0, 3, 8, 16} {
		for q := 0; q < 50; q++ {
			query := hashes[rng.Intn(n)]

			want := map[int64]bool{}
			for i, h := range hashes {
				if HammingDistance(query, h) <= radius {
					want[int64(i)] = true
				}
			}

			got := map[int64]bool{}
			for _, m := range tree.Within(query, radius) {
				if HammingDistance(query, hashes[m.Value]) != m.Dist {
					t.Fatalf("reported dist %d wrong for value %d", m.Dist, m.Value)
				}
				got[m.Value] = true
			}

			if len(got) != len(want) {
				t.Fatalf("radius %d: got %d matches, want %d", radius, len(got), len(want))
			}
			for v := range want {
				if !got[v] {
					t.Fatalf("radius %d: missing match %d", radius, v)
				}
			}
		}
	}
}
