package report

import "github.com/alizdavoodi/herdr-context-usage/internal/provider"

func ShouldWrite(kind provider.Kind, value int64) bool {
	return kind != provider.Claude || value != 0
}
