package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLI(t *testing.T) {
	config := "examples/permissions.json"
	for _, tc := range []struct {
		args     []string
		input    string
		code     int
		contains string
	}{
		{[]string{"check", "--config", config, "--shell", "zsh", `echo "$(git push)"`}, "", 0, `"ask":[["git","commit|push"]]`},
		{[]string{"check", "--config", config, "git status"}, "", 0, `"ask":[]`},
		{[]string{"check", "--config", config, "echo '"}, "", 1, ""},
		{[]string{"hook", "--config", config}, `{"hook_event_name":"PreToolUse","tool_name":"Bash","cwd":"/tmp","tool_input":{"command":"git status"}}`, 0, "{}"},
		{[]string{"hook", "--config", config}, "invalid", 0, `"permissionDecision":"deny"`},
		{[]string{"hook", "--config", config, "--timeout", "121s"}, "", 2, ""},
		{[]string{"hook"}, "", 2, ""},
		{[]string{"hook-config", "--config", config, "--shell", "zsh"}, "", 0, "scripts/hook.sh"},
	} {
		var out, diagnostic bytes.Buffer
		code := run(context.Background(), tc.args, strings.NewReader(tc.input), &out, &diagnostic)
		if code != tc.code || !strings.Contains(out.String(), tc.contains) {
			t.Errorf("%v: code=%d out=%s err=%s", tc.args, code, &out, &diagnostic)
		}
		if code == 0 && !json.Valid(out.Bytes()) {
			t.Errorf("stdout is not JSON: %s", &out)
		}
	}
}

func TestHookWrapperFailure(t *testing.T) {
	dir := t.TempDir()
	scripts := filepath.Join(dir, "scripts")
	os.Mkdir(scripts, 0755)
	data, err := os.ReadFile("scripts/hook.sh")
	if err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(scripts, "hook.sh")
	os.WriteFile(entry, data, 0755)
	for _, binary := range []string{"", "#!/bin/sh\nexit 1\n", "#!/bin/sh\nprintf '{}\\n'\n"} {
		if binary != "" {
			os.WriteFile(filepath.Join(dir, "herdr-codex-confirm"), []byte(binary), 0755)
		}
		cmd := exec.Command("sh", entry)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		if strings.Contains(binary, "printf") {
			if err != nil || stdout.String() != "{}\n" {
				t.Fatalf("allow: %v %s", err, &stdout)
			}
		} else if e, ok := err.(*exec.ExitError); !ok || e.ExitCode() != 2 || stderr.Len() == 0 {
			t.Fatalf("did not block failed binary: %v %s", err, &stderr)
		}
	}
}

func TestGeneratedHookMissingScript(t *testing.T) {
	var output, diagnostic bytes.Buffer
	code := run(context.Background(), []string{"hook-config", "--config", "examples/permissions.json"}, strings.NewReader(""), &output, &diagnostic)
	if code != 0 {
		t.Fatalf("hook-config failed: %s", &diagnostic)
	}
	var definition struct {
		Hooks struct {
			PreToolUse []struct {
				Hooks []struct {
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"PreToolUse"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(output.Bytes(), &definition); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := definition.Hooks.PreToolUse[0].Hooks[0].Command
	script := filepath.Join(filepath.Dir(exe), "scripts", "hook.sh")
	missing := filepath.Join(t.TempDir(), "removed plugin", "scripts", "hook.sh")
	command = strings.Replace(command, quote(script), quote(missing), 1)
	cmd := exec.Command("sh", "-c", command)
	data, err := cmd.CombinedOutput()
	if e, ok := err.(*exec.ExitError); !ok || e.ExitCode() != 2 || len(data) == 0 {
		t.Fatalf("missing hook script did not block execution: %v %s", err, data)
	}
}

func TestShellQuoting(t *testing.T) {
	value := "spaces ' quotes \" $(touch /tmp/never-run-confirm-test) `false`\nnext"
	output, err := exec.Command("sh", "-c", "printf '%s' "+quote(value)).Output()
	if err != nil || string(output) != value {
		t.Fatalf("shell quoting changed data: %q %v", output, err)
	}
}
