package herdr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
)

const SourceID = "herdr-context-usage"

type Agent struct {
	Agent         string                 `json:"agent"`
	ForegroundCWD string                 `json:"foreground_cwd"`
	PaneID        string                 `json:"pane_id"`
	TerminalTitle string                 `json:"terminal_title"`
	Tokens        map[string]string      `json:"tokens"`
	AgentSession  *provider.AgentSession `json:"agent_session"`
}

type Pane struct {
	PaneID string            `json:"pane_id"`
	Tokens map[string]string `json:"tokens"`
}

type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type Client struct {
	binary string
	runner Runner
}

func NewClient(binary string, runner Runner) *Client {
	if binary == "" {
		binary = "herdr"
	}
	if runner == nil {
		runner = execRunner{}
	}
	return &Client{binary: binary, runner: runner}
}

func (c *Client) ListAgents(ctx context.Context) ([]Agent, error) {
	data, err := c.runner.Run(ctx, c.binary, "agent", "list")
	if err != nil {
		return nil, fmt.Errorf("list herdr agents: %w", err)
	}
	var response struct {
		Result struct {
			Agents []Agent `json:"agents"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("decode herdr agent list: %w", err)
	}
	return response.Result.Agents, nil
}

func (c *Client) ListPanes(ctx context.Context) ([]Pane, error) {
	data, err := c.runner.Run(ctx, c.binary, "pane", "list")
	if err != nil {
		return nil, fmt.Errorf("list herdr panes: %w", err)
	}
	var response struct {
		Result struct {
			Panes []Pane `json:"panes"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("decode herdr pane list: %w", err)
	}
	return response.Result.Panes, nil
}

func (c *Client) ProcessInfo(ctx context.Context, paneID string) ([]provider.Process, error) {
	data, err := c.runner.Run(ctx, c.binary, "pane", "process-info", "--pane", paneID)
	if err != nil {
		return nil, fmt.Errorf("get process info for pane %s: %w", paneID, err)
	}
	var response struct {
		Result struct {
			ProcessInfo struct {
				Processes []provider.Process `json:"foreground_processes"`
			} `json:"process_info"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("decode process info for pane %s: %w", paneID, err)
	}
	return response.Result.ProcessInfo.Processes, nil
}

// Report and Clear carry the caller's sequence because Herdr does not
// serialize plugin runs: an older, slower run can finish after a newer one,
// and Herdr ignores a report whose sequence is not above the last one it
// accepted for this source.
func (c *Client) Report(ctx context.Context, paneID, display, raw string, seq int64) error {
	_, err := c.runner.Run(ctx, c.binary,
		"pane", "report-metadata", paneID,
		"--source", SourceID,
		"--token", "ctx="+display,
		"--token", "ctx_raw="+raw,
		"--seq", strconv.FormatInt(seq, 10),
	)
	if err != nil {
		return fmt.Errorf("report context occupancy for pane %s: %w", paneID, err)
	}
	return nil
}

func (c *Client) Clear(ctx context.Context, paneID string, seq int64) error {
	_, err := c.runner.Run(ctx, c.binary,
		"pane", "report-metadata", paneID,
		"--source", SourceID,
		"--clear-token", "ctx",
		"--clear-token", "ctx_raw",
		"--seq", strconv.FormatInt(seq, 10),
	)
	if err != nil {
		return fmt.Errorf("clear context occupancy for pane %s: %w", paneID, err)
	}
	return nil
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	output, err := command.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			stderr := strings.TrimSpace(string(exitErr.Stderr))
			if stderr != "" {
				return nil, fmt.Errorf("%s: %w: %s", name, err, stderr)
			}
		}
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return output, nil
}
