package report

import (
	"testing"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
)

func TestShouldWrite(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider provider.Kind
		value    int64
		want     bool
	}{
		{name: "positive measurement", provider: provider.Claude, value: 92_481, want: true},
		{name: "synthetic Claude zero is rejected defensively", provider: provider.Claude, value: 0, want: false},
		{name: "Codex zero remains representable", provider: provider.Codex, value: 0, want: true},
		{name: "OpenCode zero remains representable", provider: provider.OpenCode, value: 0, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ShouldWrite(tt.provider, tt.value); got != tt.want {
				t.Fatalf("ShouldWrite() = %v, want %v", got, tt.want)
			}
		})
	}
}
