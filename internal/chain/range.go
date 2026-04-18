package chain

import "fmt"

// BlockRange is an inclusive [Start, End] interval of block numbers.
// Inclusive semantics matches how humans describe ranges ("blocks 100
// through 200") and how sampling strategies iterate.
type BlockRange struct {
	Start BlockNumber
	End   BlockNumber
}

// NewBlockRange constructs a range and rejects inverted bounds so
// downstream Contains/Len can assume well-formedness.
func NewBlockRange(start, end BlockNumber) (BlockRange, error) {
	if start > end {
		return BlockRange{}, fmt.Errorf(
			"block range: start %d must be <= end %d", start.Uint64(), end.Uint64())
	}
	return BlockRange{Start: start, End: end}, nil
}

// Len returns the inclusive count of blocks in the range: [5,5] = 1,
// [5,15] = 11. End-Start+1 never overflows uint64 within a validated
// range because we rejected start > end at construction.
func (r BlockRange) Len() uint64 {
	return r.End.Uint64() - r.Start.Uint64() + 1
}

// Contains reports whether n falls within [Start, End] inclusive.
func (r BlockRange) Contains(n BlockNumber) bool {
	return n >= r.Start && n <= r.End
}

// String returns a compact "[start..end]" form for logs.
func (r BlockRange) String() string {
	return fmt.Sprintf("[%d..%d]", r.Start.Uint64(), r.End.Uint64())
}
