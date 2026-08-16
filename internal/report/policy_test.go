package report

import "testing"

func TestShouldWrite(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value int64
		want  bool
	}{
		{name: "positive measurement", value: 92_481, want: true},
		{name: "zero is never a valid Claude measurement", value: 0, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ShouldWrite(tt.value); got != tt.want {
				t.Fatalf("ShouldWrite() = %v, want %v", got, tt.want)
			}
		})
	}
}
