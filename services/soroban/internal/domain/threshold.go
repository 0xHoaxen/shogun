package domain

import "slices"

// percent is the divisor of a threshold such as 80 (percent).
const percent = 100

// ThresholdsReached returns, ascending, the thresholds (percentages of limit)
// that spent has reached. A budget with no limit has no thresholds to reach.
func ThresholdsReached(thresholds []int32, spent, limit int64) []int32 {
	if limit <= 0 {
		return nil
	}
	var reached []int32
	for _, t := range thresholds {
		if spent*percent >= int64(t)*limit {
			reached = append(reached, t)
		}
	}
	slices.Sort(reached)
	return reached
}
