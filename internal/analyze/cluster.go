package analyze

import (
	"github.com/go-birds/photosift/internal/index"
	"github.com/go-birds/photosift/internal/sim"
)

// unionFind is a standard disjoint-set with path compression and union by rank.
type unionFind struct {
	parent []int
	rank   []int
}

func newUnionFind(n int) *unionFind {
	u := &unionFind{parent: make([]int, n), rank: make([]int, n)}
	for i := range u.parent {
		u.parent[i] = i
	}
	return u
}

func (u *unionFind) find(x int) int {
	for u.parent[x] != x {
		u.parent[x] = u.parent[u.parent[x]]
		x = u.parent[x]
	}
	return x
}

func (u *unionFind) union(a, b int) {
	ra, rb := u.find(a), u.find(b)
	if ra == rb {
		return
	}
	if u.rank[ra] < u.rank[rb] {
		ra, rb = rb, ra
	}
	u.parent[rb] = ra
	if u.rank[ra] == u.rank[rb] {
		u.rank[ra]++
	}
}

// hashIndex is a perceptual-hash BK-tree paired with the images it indexes.
// Building it is the expensive part; near-duplicate and "same subject" both
// need clustering at different radii, so we build once and query twice.
type hashIndex struct {
	tree sim.BKTree
	imgs []index.Image
}

func newHashIndex(imgs []index.Image) *hashIndex {
	h := &hashIndex{imgs: imgs}
	for i, im := range imgs {
		h.tree.Add(im.Dhash, int64(i))
	}
	return h
}

// clusters returns groups of image indices whose perceptual hashes are within
// maxDist of one another (transitively). Singletons are omitted.
func (h *hashIndex) clusters(maxDist int) [][]int {
	n := len(h.imgs)
	uf := newUnionFind(n)
	for i := 0; i < n; i++ {
		for _, m := range h.tree.Within(h.imgs[i].Dhash, maxDist) {
			uf.union(i, int(m.Value))
		}
	}

	groups := map[int][]int{}
	for i := 0; i < n; i++ {
		r := uf.find(i)
		groups[r] = append(groups[r], i)
	}

	var out [][]int
	for _, g := range groups {
		if len(g) > 1 {
			out = append(out, g)
		}
	}
	return out
}

// bestInGroup returns the index (within group) of the image worth keeping:
// highest resolution, then sharpest, then earliest taken, then shortest path.
func bestInGroup(imgs []index.Image, group []int) int {
	best := group[0]
	for _, idx := range group[1:] {
		if better(imgs[idx], imgs[best]) {
			best = idx
		}
	}
	return best
}

// better reports whether a is a better keeper than b.
func better(a, b index.Image) bool {
	if a.Pixels() != b.Pixels() {
		return a.Pixels() > b.Pixels()
	}
	if a.BlurVar != b.BlurVar {
		return a.BlurVar > b.BlurVar
	}
	if a.TakenAtUnix != b.TakenAtUnix {
		return a.TakenAtUnix < b.TakenAtUnix
	}
	return len(a.Path) < len(b.Path)
}
