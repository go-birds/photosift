package sim

// BKTree indexes uint64 perceptual hashes for fast "find everything within
// Hamming distance r" queries. Because Hamming distance is a metric, the
// triangle inequality lets a lookup prune whole subtrees, turning what would be
// an O(n) scan per query into roughly O(log n) for small radii. This keeps
// near-duplicate clustering tractable on libraries with hundreds of thousands
// of photos.
type BKTree struct {
	root *bkNode
	size int
}

type bkNode struct {
	hash     uint64
	value    int64 // caller payload, e.g. image ID
	children map[int]*bkNode
}

// Add inserts a hash with an associated value (typically an image ID).
func (t *BKTree) Add(hash uint64, value int64) {
	n := &bkNode{hash: hash, value: value}
	if t.root == nil {
		t.root = n
		t.size = 1
		return
	}
	cur := t.root
	for {
		d := HammingDistance(cur.hash, hash)
		if cur.children == nil {
			cur.children = map[int]*bkNode{}
		}
		child, ok := cur.children[d]
		if !ok {
			cur.children[d] = n
			t.size++
			return
		}
		cur = child
	}
}

// Match is a hash within the query radius.
type Match struct {
	Value int64
	Dist  int
}

// Within returns all stored entries whose hash is within maxDist of hash.
func (t *BKTree) Within(hash uint64, maxDist int) []Match {
	var out []Match
	if t.root == nil {
		return out
	}
	stack := []*bkNode{t.root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		d := HammingDistance(n.hash, hash)
		if d <= maxDist {
			out = append(out, Match{Value: n.value, Dist: d})
		}
		lo, hi := d-maxDist, d+maxDist
		for cd, child := range n.children {
			if cd >= lo && cd <= hi {
				stack = append(stack, child)
			}
		}
	}
	return out
}

func (t *BKTree) Len() int { return t.size }
