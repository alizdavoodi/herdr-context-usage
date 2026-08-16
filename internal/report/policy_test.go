package report

import "testing"

func TestShouldWrite(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		value    int64
		existing map[string]string
		want     bool
	}{
		{name: "positive measurement", value: 92_481, want: true},
		{name: "zero is reportable when no good value exists", value: 0, want: true},
		{name: "zero never overwrites a good value", value: 0, existing: map[string]string{"ctx_raw": "92481"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ShouldWrite(tt.value, tt.existing); got != tt.want {
				t.Fatalf("ShouldWrite() = %v, want %v", got, tt.want)
			}
		})
	}
}
