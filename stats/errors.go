package stats

type statsError struct {
	err string
}

func (s statsError) Error() string {
	return s.err
}

func (s statsError) String() string {
	return s.err
}

// These are the package-wide error values.
// All error identification should use these values.
// https://github.com/golang/go/wiki/Errors#naming
var (
	// ErrEmptyInput Input must not be empty
	EmptyInputErr = statsError{"Input must not be empty."}
	// ErrNaN Not a number
	NaNErr = statsError{"Not a number."}
	// ErrNegative Must not contain negative values
	NegativeErr = statsError{"Must not contain negative values."}
	// ErrZero Must not contain zero values
	ZeroErr = statsError{"Must not contain zero values."}
	// ErrBounds Input is outside of range
	BoundsErr = statsError{"Input is outside of range."}
	// ErrSize Must be the same length
	SizeErr = statsError{"Must be the same length."}
	// ErrInfValue Value is infinite
	InfValueErr = statsError{"Value is infinite."}
	// ErrYCoord Y Value must be greater than zero
	YCoordErr = statsError{"Y Value must be greater than zero."}
)
