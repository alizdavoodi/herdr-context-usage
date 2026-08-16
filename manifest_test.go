package herdr_context_usage

import (
	"os"
	"strings"
	"testing"
)

func TestManifestCoversAgentLifecycleWithPortableCommands(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("herdr-plugin.toml")
	if err != nil {
		t.Fatal(err)
	}
	manifest := string(data)
	for _, event := range []string{"pane.agent_status_changed", "pane.focused", "pane.exited", "pane.closed"} {
		if !strings.Contains(manifest, `on = "`+event+`"`) {
			t.Errorf("manifest does not subscribe to %s", event)
		}
	}
	if !strings.Contains(manifest, `command = ["$HERDR_PLUGIN_ROOT/herdr-context-usage"]`) {
		t.Error("manifest commands do not use HERDR_PLUGIN_ROOT")
	}
	if strings.Contains(manifest, `command = ["./herdr-context-usage"]`) {
		t.Error("manifest retains working-directory-dependent command")
	}
}
