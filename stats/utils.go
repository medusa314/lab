package stats

import (
	"sort"
	"time"
)

// float64ToInt rounds a float64 to an int
func float64ToInt(input float64) (output int) {
	r, _ := Round(input, 0)
	return int(r)
}

// unixnano returns nanoseconds from UTC epoch
func unixnano() int64 {
	return time.Now().UTC().UnixNano()
}

// copyslice copies a slice of float64s
func copyslice(input []float64) []float64 {
	s := make([]float64, len(input))
	copy(s, input)
	return s
}

// sortedCopy returns a sorted copy of float64s
func sortedCopy(input []float64) (copy []float64) {
	copy = copyslice(input)
	sort.Float64s(copy)
	return
}

// sortedCopyDif returns a sorted copy of float64s
// only if the original data isn't sorted.
// Only use this if returned slice won't be manipulated!
func sortedCopyDif(input []float64) (copy []float64) {
	if sort.Float64sAreSorted(input) {
		return input
	}
	copy = copyslice(input)
	sort.Float64s(copy)
	return
}
