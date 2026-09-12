package hook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/utahta/herdr-codex-confirm/internal/confirm"
	"github.com/utahta/herdr-codex-confirm/internal/detect"
)

type Output struct {
	Specific *Decision `json:"hookSpecificOutput,omitempty"`
}
type Decision struct {
	Event    string `json:"hookEventName"`
	Decision string `json:"permissionDecision"`
	Reason   string `json:"permissionDecisionReason"`
}

func Deny(reason string) Output { return Output{&Decision{"PreToolUse", "deny", reason}} }

func Evaluate(ctx context.Context, input io.Reader, config, shell string, ask func(context.Context, confirm.Request) error) Output {
	data, err := io.ReadAll(io.LimitReader(input, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return Deny("Cannot read hook input or input exceeds 1 MiB.")
	}
	var payload struct {
		Event string          `json:"hook_event_name"`
		Tool  string          `json:"tool_name"`
		Input json.RawMessage `json:"tool_input"`
		Cwd   string          `json:"cwd"`
	}
	if json.Unmarshal(data, &payload) != nil || payload.Event != "PreToolUse" || payload.Tool == "" {
		return Deny("Invalid PreToolUse input.")
	}
	if payload.Tool != "Bash" {
		return Output{}
	}
	var command struct {
		Command *string `json:"command"`
	}
	if json.Unmarshal(payload.Input, &command) != nil || command.Command == nil || payload.Cwd == "" {
		return Deny("Bash input requires tool_input.command and cwd strings.")
	}
	rules, err := detect.Load(config)
	if err != nil {
		return Deny(fmt.Sprintf("Confirmation configuration error: %s", err))
	}
	matches, err := detect.Match(*command.Command, shell, rules)
	if err != nil {
		return Deny(fmt.Sprintf("Command analysis failed: %s.", err))
	}
	if len(matches) == 0 {
		return Output{}
	}
	if err := ask(ctx, confirm.Request{Command: *command.Command, Cwd: payload.Cwd, Rules: matches}); err != nil {
		return Deny("Command was not approved: " + err.Error())
	}
	return Output{}
}
