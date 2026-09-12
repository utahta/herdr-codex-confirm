package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/utahta/herdr-codex-confirm/internal/confirm"
	"github.com/utahta/herdr-codex-confirm/internal/detect"
	"github.com/utahta/herdr-codex-confirm/internal/hook"
	"github.com/utahta/herdr-codex-confirm/internal/tui"
)

var version = "0.1.0"

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer cancel()
	os.Exit(run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, input io.Reader, output, diagnostics io.Writer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(output, "Usage: herdr-codex-confirm <hook|popup|check|hook-config> [options]\n\n  hook         Read a Codex PreToolUse JSON request from stdin\n  popup        Show the pending request (launched by Herdr)\n  check        Print matching rules without opening a popup or executing commands\n  hook-config  Print the Codex hook definition for this installation\n\nUse <command> --help for options. No command executes the submitted shell text.")
		return 0
	}
	if args[0] == "--version" {
		fmt.Fprintln(output, "herdr-codex-confirm "+version)
		return 0
	}
	if args[0] == "popup" {
		if len(args) != 1 {
			fmt.Fprintln(diagnostics, "popup accepts no arguments; it is launched by a hook")
			return 1
		}
		if err := confirm.Popup(ctx, os.Getenv("CODEX_CONFIRM_SOCKET"), os.Getenv("CODEX_CONFIRM_ID"), tui.Show); err != nil {
			fmt.Fprintln(diagnostics, tui.Safe(err.Error()))
			return 1
		}
		return 0
	}
	subcommand := args[0]
	if subcommand != "hook" && subcommand != "check" && subcommand != "hook-config" {
		fmt.Fprintln(diagnostics, "unknown command; use --help")
		return 2
	}
	fs := flag.NewFlagSet(subcommand, flag.ContinueOnError)
	fs.SetOutput(diagnostics)
	config := fs.String("config", "", "path to permissions.json (required)")
	shell := fs.String("shell", "bash", "shell syntax: bash, sh, or zsh (experimental)")
	state := fs.String("state-dir", "", "absolute runtime state directory (default: Herdr plugin state directory)")
	timeout := fs.Duration("timeout", 120*time.Second, "total confirmation timeout, including UI busy wait (maximum 120s)")
	if err := fs.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if *config == "" || *timeout <= 0 || *timeout > 120*time.Second {
		fmt.Fprintln(diagnostics, "--config is required and --timeout must be greater than zero and at most 120s")
		return 2
	}
	if _, err := detect.Variant(*shell); err != nil {
		fmt.Fprintln(diagnostics, err)
		return 2
	}
	if subcommand == "check" {
		if fs.NArg() != 1 {
			fmt.Fprintln(diagnostics, "check requires one quoted COMMAND after the options")
			return 2
		}
		rules, err := detect.Load(*config)
		if err != nil {
			fmt.Fprintln(diagnostics, tui.Safe(err.Error()))
			return 1
		}
		matches, err := detect.Match(fs.Arg(0), *shell, rules)
		if err != nil {
			fmt.Fprintln(diagnostics, err)
			return 1
		}
		if json.NewEncoder(output).Encode(map[string]any{"ask": matches}) != nil {
			return 1
		}
		return 0
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(diagnostics, "unexpected positional arguments")
		return 2
	}
	if subcommand == "hook-config" {
		if _, err := detect.Load(*config); err != nil {
			fmt.Fprintln(diagnostics, tui.Safe(err.Error()))
			return 1
		}
		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintln(diagnostics, err)
			return 1
		}
		configPath, err := filepath.Abs(*config)
		if err != nil {
			fmt.Fprintln(diagnostics, err)
			return 1
		}
		command := "sh " + quote(filepath.Join(filepath.Dir(exe), "scripts", "hook.sh")) + " --config " + quote(configPath) + " --shell " + quote(*shell) + " --timeout " + quote(timeout.String())
		if *state != "" {
			if !filepath.IsAbs(*state) {
				fmt.Fprintln(diagnostics, "--state-dir must be absolute")
				return 1
			}
			command += " --state-dir " + quote(*state)
		}
		command += " || exit 2"
		definition := map[string]any{"hooks": map[string]any{"PreToolUse": []any{map[string]any{"matcher": "^Bash$", "hooks": []any{map[string]any{"type": "command", "command": command, "timeout": 150, "statusMessage": "Checking command confirmation"}}}}}}
		enc := json.NewEncoder(output)
		enc.SetIndent("", "  ")
		if enc.Encode(definition) != nil {
			return 1
		}
		return 0
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	result := hook.Evaluate(ctx, input, *config, *shell, func(ctx context.Context, req confirm.Request) error {
		req.SourcePane = os.Getenv("HERDR_PANE_ID")
		dir := *state
		if dir == "" {
			var err error
			dir, err = confirm.StateDir()
			if err != nil {
				return err
			}
		}
		return confirm.Ask(ctx, dir, req, confirm.OpenHerdr)
	})
	if json.NewEncoder(output).Encode(result) != nil {
		fmt.Fprintln(diagnostics, "could not write hook result")
		return 2
	}
	return 0
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
