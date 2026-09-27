package opencodeshared

import (
	"path/filepath"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
)

// DataPath resolves one of opencode's store filenames. Both versions keep their
// databases side by side in the same directory and differ only in the name.
//
// dataHome is $XDG_DATA_HOME as main read it: opencode files its stores under
// that directory when it is set and under ~/.local/share otherwise, so it plays
// the part an agent's configured home plays for the other providers.
func DataPath(explicit, dataHome, filename string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	base, err := provider.Home(dataHome, filepath.Join(".local", "share"))
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "opencode", filename), nil
}
