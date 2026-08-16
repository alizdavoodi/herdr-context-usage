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
	if got, want := strings.Count(manifest, `command = ["./herdr-context-usage"]`), 5; got != want {
		t.Errorf("relative plugin command count = %d, want %d", got, want)
	}
	if strings.Contains(manifest, "$HERDR_PLUGIN_ROOT") {
		t.Error("manifest command arrays do not expand environment variables")
	}
}
