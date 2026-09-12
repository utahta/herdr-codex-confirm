package hook

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/utahta/herdr-codex-confirm/internal/confirm"
)

func TestEvaluate(t *testing.T) {
	config := "../detect/testdata/permissions.json"
	invalidConfig := t.TempDir() + "/permissions.json"
	if err := os.WriteFile(invalidConfig, []byte(`{"patterns":[["git","["]]}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, input, config string
		ask, deny           bool
		failure             error
	}{
		{"allow", payload(`git push`), config, true, false, nil},
		{"deny", payload(`git push`), config, true, true, errors.New("user denied")},
		{"timeout", payload(`git push`), config, true, true, context.DeadlineExceeded},
		{"read", payload(`git status`), config, false, false, nil},
		{"api", payload(`gh api graphql -f 'query=mutation {}'`), config, false, false, nil},
		{"compound", payload(`git push; git commit -m ok`), config, true, false, nil},
		{"change directory", payload(`cd /foo/bar && git push`), config, true, false, nil},
		{"brace expansion", payload(`cd /foo/bar && git {push,-v} origin main`), config, true, false, nil},
		{"brace expansion denied", payload(`git {push,-v}`), config, true, true, errors.New("user denied")},
		{"brace expansion limit", payload(`git {push,-v}{1..1000000}`), config, false, true, nil},
		{"invalid regex", payload(`git status`), invalidConfig, false, true, nil},
		{"invalid JSON", "{", config, false, true, nil},
		{"null", "null", config, false, true, nil},
		{"invalid event", `{"hook_event_name":"PostToolUse","tool_name":"Bash"}`, config, false, true, nil},
		{"missing command", `{"hook_event_name":"PreToolUse","tool_name":"Bash","cwd":"/tmp","tool_input":{}}`, config, false, true, nil},
		{"bad config", payload(`git push`), "missing", false, true, nil},
		{"parse failure", payload(`git 'push`), config, false, true, nil},
		{"other tool", `{"hook_event_name":"PreToolUse","tool_name":"apply_patch"}`, "missing", false, false, nil},
		{"multiple inputs", payload(`git status`) + payload(`git push`), config, false, true, nil},
		{"oversized", strings.Repeat("x", (1<<20)+1), config, false, true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			result := Evaluate(context.Background(), strings.NewReader(tc.input), tc.config, "zsh", func(_ context.Context, req confirm.Request) error {
				calls++
				if req.Cwd != "/tmp/repo" || !reflect.DeepEqual(req.Rules, [][]string{{"git", "push|commit"}}) {
					t.Errorf("lost request data: %+v", req)
				}
				var original struct {
					Input struct {
						Command string `json:"command"`
					} `json:"tool_input"`
				}
				json.Unmarshal([]byte(tc.input), &original)
				if req.Command != original.Input.Command {
					t.Error("command was rewritten")
				}
				return tc.failure
			})
			if (calls == 1) != tc.ask || calls > 1 {
				t.Fatalf("ask called %d times", calls)
			}
			if (result.Specific != nil) != tc.deny {
				t.Fatalf("unexpected output: %+v", result)
			}
			if tc.deny && (result.Specific.Decision != "deny" || result.Specific.Event != "PreToolUse" || result.Specific.Reason == "") {
				t.Errorf("invalid denial: %+v", result)
			}
		})
	}
}

func payload(command string) string {
	data, _ := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Bash", "cwd": "/tmp/repo", "tool_input": map[string]string{"command": command}})
	return string(data)
}

func TestFeedbackRoundTrip(t *testing.T) {
	feedback := "先にテストを追加してください。\n\"git push\" は確認後に。\nKeep this punctuation..."
	for _, tc := range []struct {
		name   string
		result confirm.Result
		uiErr  error
		reason string
	}{
		{"feedback", confirm.Result{Feedback: feedback}, nil, "command was denied by the user.\nUser feedback:\n" + feedback},
		{"whitespace", confirm.Result{Feedback: " \n "}, nil, "command was denied by the user"},
		{"plain deny", confirm.Result{}, nil, "command was denied by the user"},
		{"approve", confirm.Result{Approved: true, Feedback: feedback}, nil, ""},
		{"UI failure", confirm.Result{Feedback: feedback}, errors.New("UI failed"), "confirmation UI failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, err := os.MkdirTemp("/tmp", "hook-feedback-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(state)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			done := make(chan error, 1)
			output := Evaluate(ctx, strings.NewReader(payload("git push")), "../detect/testdata/permissions.json", "zsh", func(ctx context.Context, req confirm.Request) error {
				return confirm.Ask(ctx, state, req, func(_ context.Context, socket, id string) error {
					go func() {
						done <- confirm.Popup(ctx, socket, id, func(context.Context, confirm.Request) (confirm.Result, error) {
							return tc.result, tc.uiErr
						})
					}()
					return nil
				})
			})
			select {
			case err := <-done:
				if !errors.Is(err, tc.uiErr) {
					t.Fatalf("popup error: %v", err)
				}
			case <-ctx.Done():
				t.Fatalf("popup did not finish: %v; output: %+v", ctx.Err(), output)
			}
			data, err := json.Marshal(output)
			if err != nil {
				t.Fatal(err)
			}
			if tc.reason == "" {
				if string(data) != "{}" {
					t.Fatalf("approval contains feedback: %s", data)
				}
				return
			}
			var decoded Output
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.Specific == nil || decoded.Specific.Event != "PreToolUse" || decoded.Specific.Decision != "deny" || decoded.Specific.Reason != "Command was not approved: "+tc.reason {
				t.Fatalf("feedback changed in hook JSON: %s", data)
			}
		})
	}
}
