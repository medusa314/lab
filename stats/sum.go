package stats

import "math"

// Sum adds all the numbers of a slice together
func Sum(input []float64) (sum float64, err error) {

	if len(input) == 0 {
		return math.NaN(), EmptyInputErr
	}

	// Add em up
	for _, n := range input {
		sum += n
	}

	return sum, nil
}
