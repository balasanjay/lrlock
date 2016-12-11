package lrlock

// From http://graphics.stanford.edu/~seander/bithacks.html#RoundUpPowerOf2
func roundNearestPowerOf2(v int) int {
	v--
	v |= v >> 1
	v |= v >> 2
	v |= v >> 4
	v |= v >> 8
	v |= v >> 16
	v++
	return v
}
