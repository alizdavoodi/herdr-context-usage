package format

import (
	"fmt"
	"math"
)

// Occupancy keeps the sidebar compact while ctx_raw remains the exact machine value.
func Occupancy(tokens int64) string {
	switch {
	case tokens < 1_000:
		return fmt.Sprintf("%d", tokens)
	case tokens < 999_500:
		return fmt.Sprintf("%.0fk", float64(tokens)/1_000)
	default:
		millions := math.Round(float64(tokens)/100_000) / 10
		return fmt.Sprintf("%.1fM", millions)
	}
}
