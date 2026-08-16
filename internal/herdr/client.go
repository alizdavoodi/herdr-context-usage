package herdr

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
)

const SourceID = "herdr-context-usage"

type AgentSession struct {
	Source string `json:"source"`
	Agent  string `json:"agent"`
	Kind   string `json:"kind"`
	Value  string `json:"value"`
}

type Agent struct {
	Agent         string            `json:"agent"`
	AgentStatus   string            `json:"agent_status"`
	ForegroundCWD string            `json:"foreground_cwd"`
	PaneID        string            `json:"pane_id"`
	Tokens        map[string]string `json:"tokens"`
	AgentSession  *AgentSession     `json:"agent_session"`
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

func (c *Client) Report(ctx context.Context, paneID, display, raw string) error {
	_, err := c.runner.Run(ctx, c.binary,
		"pane", "report-metadata", paneID,
		"--source", SourceID,
		"--token", "ctx="+display,
		"--token", "ctx_raw="+raw,
	)
	if err != nil {
		return fmt.Errorf("report context occupancy for pane %s: %w", paneID, err)
	}
	return nil
}

func (c *Client) Clear(ctx context.Context, paneID string) error {
	_, err := c.runner.Run(ctx, c.binary,
		"pane", "report-metadata", paneID,
		"--source", SourceID,
		"--clear-token", "ctx",
		"--clear-token", "ctx_raw",
	)
	if err != nil {
		return fmt.Errorf("clear context occupancy for pane %s: %w", paneID, err)
	}
	return nil
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s: %w: %s", name, err, output)
	}
	return output, nil
}
