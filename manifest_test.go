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
	if got, want := strings.Count(manifest, `command = ["./herdr-context-usage"]`), 6; got != want {
		t.Errorf("relative plugin command count = %d, want %d", got, want)
	}
	// Agent hooks invoke the action by this id, so renaming it silently breaks
	// every hook a user has installed.
	if _, actions, ok := strings.Cut(manifest, "[[actions]]"); !ok || !strings.Contains(actions, `id = "refresh"`) {
		t.Error(`manifest does not declare the "refresh" action`)
	}
	if strings.Contains(manifest, "$HERDR_PLUGIN_ROOT") {
		t.Error("manifest command arrays do not expand environment variables")
	}
}
