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

// clusterByHash groups images whose perceptual hashes are within maxDist of one
// another (transitively) using a BK-tree to find neighbours and union-find to
// merge them. It returns groups of indices into imgs; singletons are omitted.
func clusterByHash(imgs []index.Image, maxDist int) [][]int {
	n := len(imgs)
	uf := newUnionFind(n)

	var tree sim.BKTree
	for i, img := range imgs {
		for _, m := range tree.Within(img.Dhash, maxDist) {
			uf.union(i, int(m.Value))
		}
		tree.Add(img.Dhash, int64(i))
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
