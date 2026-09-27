package app

import (
	"encoding/json"
	"os"
	"testing"
)

func TestFormatOccupancy(t *testing.T) {
	t.Parallel()

	var tests []struct {
		Name string `json:"name"`
		In   int64  `json:"tokens"`
		Want string `json:"want"`
	}
	data, err := os.ReadFile("testdata/occupancy.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &tests); err != nil {
		t.Fatal(err)
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			if got := formatOccupancy(tt.In); got != tt.Want {
				t.Fatalf("formatOccupancy(%d) = %q, want %q", tt.In, got, tt.Want)
			}
		})
	}
}
