package components

// StepCursor moves a list cursor by delta, stopping at either end rather than
// wrapping. Every list in the TUI does this, and each hand-rolled copy was one
// off-by-one away from letting the cursor sit on a row that is not there.
func StepCursor(index, delta, maxIndex int) int {
	return min(max(index+delta, 0), max(maxIndex, 0))
}

// CycleIndex moves a filter index by delta and wraps, which is what a filter cycled
// with a single key has to do. delta may be negative or larger than n.
func CycleIndex(index, delta, n int) int {
	if n <= 0 {
		return 0
	}
	return ((index+delta)%n + n) % n
}
