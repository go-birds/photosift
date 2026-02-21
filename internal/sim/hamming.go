// write a function to calculate the Hamming distance between two uint64 numbers
package sim

// HammingDistance calculates the Hamming distance between two uint64 numbers.
// The Hamming distance is defined as the number of positions at which the corresponding bits are different.
func HammingDistance(a, b uint64) int {
	x := a ^ b // bitwise exclusive or (XOR)
	count := 0
	for x != 0 {
		count += int(x & 1) // bitwise AND with 1 returns 1 iff the least significant bit is 1
		x >>= 1             // shorthand to right-shift by 1 bit (drop least significant bit)
	}
	return count
}
