# Codex Confirm

Review selected Codex shell commands in a Herdr popup before they run. Choose **Approve once** to continue, **Deny** to block the tool call, or **Deny with feedback** to tell Codex what to change. Your usual Codex sandbox and approval checks still apply.

**Setup requires both plugin installation and a Codex hook.** Installing this Herdr plugin does not modify Codex's hook settings. Follow all four setup steps below.

## How it works

1. Codex calls the plugin through a synchronous `PreToolUse` hook before running a shell command.
2. The plugin checks the command against your patterns. Matching commands open a Herdr popup showing the entire command, working directory, source pane, and matched patterns.
3. The plugin waits for your answer and returns the result to Codex. Commands that do not match continue without a popup.

Codex supplies the execution event; Herdr hosts the confirmation UI. The plugin never executes or rewrites the submitted command. One confirmation covers the whole tool call, including compound commands. Approvals are not saved or reused.

## Requirements

- macOS or Linux, on arm64 or amd64.
- Herdr 0.8.0 or later, with Codex running in a pane in its normal session UI.
- Codex CLI with synchronous `PreToolUse` command hooks.
- curl, ssh-keygen with `-Y verify` support, and either shasum or sha256sum for downloading release binaries. If Go is available, the installer builds automatically from source and requires Go 1.26 or later.

See [verified environments](docs/development.md#verified-environments) for tested platforms and remaining verification work.

## Setup

### 1. Install the Herdr plugin

Run this command in a Herdr terminal:

```sh
herdr plugin install utahta/herdr-codex-confirm
```

Herdr installs the plugin and runs its installer to prepare the executable. To select a published version, add `--ref v0.1.0`. For binary downloads, the installer verifies the OpenSSH signature on the checksum list using the public key included with the plugin, then checks the archive's SHA-256 checksum. Verification failures stop installation.

Check that the plugin is enabled and find its installation directory:

```sh
herdr plugin list --plugin utahta.codex-confirm --json
```

Change to the directory shown in the `plugin_root` field. Replace the example path below with that value. Run the remaining setup and check commands from this directory:

```sh
cd "/absolute/path/from/plugin_root"
```

### 2. Choose which commands need confirmation

For a new configuration, copy the example outside the installation directory so it survives plugin updates. If you already have a rule file, reuse it and substitute its path for `--config` throughout this guide:

```sh
mkdir -p "$HOME/.config/herdr-codex-confirm"
cp examples/permissions.json "$HOME/.config/herdr-codex-confirm/permissions.json"
```

Edit this file to select the commands you want to confirm. See [patterns](#patterns) for the format. The example covers Git commit/push and selected GitHub PR/issue commands; it does not cover all write operations.

### 3. Register the Codex hook

From the installation directory, generate a hook definition using the rule file you chose:

```sh
./herdr-codex-confirm hook-config \
  --config "$HOME/.config/herdr-codex-confirm/permissions.json" \
  --shell zsh > /tmp/codex-confirm-hooks.json
cat /tmp/codex-confirm-hooks.json
```

Use `--shell bash`, `--shell sh`, or `--shell zsh` to match the shell Codex executes. Bash is the default; zsh support is experimental. The generated JSON calls this installation's `scripts/hook.sh` directly using absolute paths. `hook-config` only prints JSON; it does not install or trust the hook.

**If you do not have `~/.codex/hooks.json`**, install the generated file:

```sh
mkdir -p "$HOME/.codex"
cp /tmp/codex-confirm-hooks.json "$HOME/.codex/hooks.json"
```

**If you already have `~/.codex/hooks.json`**, edit it and add the generated `hooks.PreToolUse` entry to its existing array. Preserve other events and hooks. If the file is a symlink managed by dotfiles, edit its source file.

**When replacing a confirmation hook**, replace its handler with the generated handler instead of adding a second one. Preserve other handlers in the same matcher group. Codex runs matching hooks from all configured sources, so remove any duplicate confirmation registration from project settings or inline `[hooks]` tables as well. See [OpenAI Docs: hook sources](https://learn.chatgpt.com/docs/hooks#where-codex-looks-for-hooks).

Keep `scripts/hook.sh` in the installation directory alongside the binary. Register the complete command produced by `hook-config`, including its `|| exit 2` suffix, so a missing script or executable blocks the tool call. No separate script in `~/.codex/hooks/` is needed.

### 4. Review and trust the hook in Codex

To keep using Codex's automatic approval review, retain these settings in `~/.codex/config.toml`:

```toml
approval_policy = "on-request"
approvals_reviewer = "auto_review"
```

Start a new Codex session inside Herdr and open `/hooks`. Review and trust the entry that calls this plugin's `scripts/hook.sh`. New or changed hook definitions are skipped until trusted; repeat this step after changing the registration. See [OpenAI Docs: hook trust](https://learn.chatgpt.com/docs/hooks#review-and-trust-hooks).

## Check your setup

From the installation directory, check your patterns without running the submitted command or opening a popup:

```sh
./herdr-codex-confirm check \
  --config "$HOME/.config/herdr-codex-confirm/permissions.json" \
  --shell zsh 'cd /foo/bar && git push'
```

With the example configuration, the result is `{"ask":[["git","commit|push"]]}`. An empty `{"ask":[]}` means no pattern matched. Invalid configuration or shell syntax produces an error.

For further checks, see [popup verification](docs/development.md#popup-check) and [Codex integration verification](docs/development.md#codex-integration-check).

## Patterns

```json
{
  "patterns": [
    ["git", "commit|push"],
    ["gh", "pr", "create|merge|close"],
    ["gh", "issue", "create|edit|close"]
  ]
}
```

Each array matches the beginning of a normalized command. Each string is a [Go regular expression](https://pkg.go.dev/regexp/syntax#hdr-Syntax) that must match one whole shell word; anchors are unnecessary. Additional arguments are unrestricted. A match against any pattern requests confirmation. `.*` matches a single word, not a variable number of arguments, and does not include newlines unless `(?s)` is used. JSON requires backslashes to be escaped, so a literal dot is written as `"\\."`.

| Pattern | Behavior |
| --- | --- |
| `["git", "commit\|push"]` | Matches `git commit`, `git push`, and either with arguments. |
| `["git", "push", "origin"]` | Requires `origin` immediately after `push`; later arguments are unrestricted. |
| `["gh", ".*", "create"]` | Matches `gh pr create` and `gh issue create`. |
| `["git"]` | Matches any normalized Git command. |

`["git", "commit|push"]` does not match `git commit-tree`, `git push-helper`, or `git "commit --amend"`. Quoted arguments remain single words, including any spaces inside them. The example does not ask about `git add`, `git checkout`, or `gh api`, including POST and GraphQL mutations. Your configuration file determines the confirmation targets; no personal rule set is built into the binary.

For `cd /foo/bar && git push`, the parser identifies `cd /foo/bar` and `git push` separately. The latter matches, so the popup asks once before the entire tool call begins, including `cd`. It displays the original `cd /foo/bar && git push` without rewriting it. Repeated matches of a pattern within one tool call are listed once.

### Edit your patterns

Open the file passed to `--config` in your editor. For the setup above, it is `~/.config/herdr-codex-confirm/permissions.json`. There is currently no popup shortcut for editing settings.

Changes are read on the next hook invocation. An already-open popup is not re-evaluated when the file changes. Editing only the pattern file does not change the hook definition. If you move the file or change hook options, regenerate the definition and review it in `/hooks`.

### Detection scope

The parser scans compound commands, pipelines, subshells, conditionals, loops, and statically visible function bodies. It follows command and process substitutions, including substitutions inside double quotes and unquoted here-documents. Quoted here-document bodies and display strings such as `echo "git push"` do not become executable candidates. Commands in branches or function bodies may ask even when that branch or function would not execute.

Git/GitHub executable paths and global options, leading assignments, `command`, `env`, `exec`, `nohup`, and `noglob` are normalized. Literal `sh -c`, `bash -c`, and `zsh -c` strings are parsed recursively. Quote removal does not execute substitutions. An unknown word such as `"$operation"` cannot satisfy a regex, even `.*`. Unknown arguments after the required prefix do not prevent a match: `["git", "push"]` matches `git push "$remote"`, and `["git"]` matches `git "$operation"`.

Bash and zsh brace expansions are matched as the resulting sequence of words: `git {push,-v}` matches `["git", "push"]`, and `gh {pr,create}` matches `["gh", "pr", "create"]`. Quoted or escaped braces remain literal, and POSIX mode does not expand braces. Expansion does not evaluate variables or execute commands.

Configuration, parse, and expansion errors deny the tool call, including otherwise harmless calls. Invalid regex errors identify the pattern and word using one-based positions. `{"patterns":[]}` is valid and disables matching; missing or invalid arrays and empty patterns are errors. Excessively nested commands or large brace expansions also cause an error; see [analysis limits](docs/development.md#command-analysis).

Zsh support is experimental and incomplete. POSIX mode rejects Bash constructs such as process substitutions. Unsupported syntax produces an analysis error and denial.

Aliases, dynamically constructed command names or subcommands, external scripts, `eval`, `env -S`, shell option changes, and process launches from other languages are outside the detection guarantee. The hook does not evaluate variables or source shell startup files. It is a confirmation aid for written commands, not a complete shell execution security boundary.

Untrusted or disabled hooks, Codex terminating a hook at its own timeout, existing `write_stdin` sessions, and MCP GitHub tools are outside this plugin's enforcement scope. See the [Codex hook contract](https://learn.chatgpt.com/docs/hooks#pretooluse).

## Popup and waiting

The command appears first, followed by the working directory and details about the matched patterns and source pane. The selected action is marked with `▶` and highlighted. Scroll instructions appear when the content does not fit.

- Enter confirms the selected action; the initial selection is **Deny**.
- Tab or Right selects the next action: **Deny**, **Deny with feedback**, then **Approve once**. Shift+Tab or Left selects the previous action.
- Escape, Ctrl+C, `q`, or `n` denies.
- Up/Down, PageUp/PageDown, and Home/End scroll the content. Long lines wrap without truncation.
- A terminal smaller than 36 columns by 12 rows must be enlarged before approving.

Choose **Deny with feedback** to open a text editor for instructions such as "Add tests before pushing."

- Enter inserts a newline. Ctrl+S sends the feedback and denies the command; empty or whitespace-only input denies without feedback.
- Escape returns to the action selection and keeps the draft while the popup is open. Ctrl+C denies immediately without sending the draft. The letters `q` and `n` are normal text in the editor.
- The input supports Japanese text and multiline paste. A terminal smaller than 36 columns by 12 rows must be enlarged before editing or sending feedback.

The character counter shows the 4,096-character limit, including line breaks. If the text exceeds it, the popup shows **Shorten feedback to send** and keeps the draft for editing. Ctrl+S sends nothing until the text is within the limit.

Submitted feedback is included in the denial reason returned to Codex. Returning to the action selection and choosing **Deny** or **Approve once** does not send the draft. Feedback input uses the same countdown; expiry or disconnection does not send unfinished feedback.

Control characters, carriage returns, and Unicode formatting controls are displayed as escapes. Command text cannot inject terminal escape sequences into the UI.

The popup appears in the normal UI of the hook's Herdr session. If another popup is open, the plugin waits for it to close. Launch failures, connection failures, expiry, and user denial block the command.

The default total wait is 120 seconds, including time spent waiting for the UI. Set `--timeout` when generating the hook to reduce it; the maximum is 120 seconds. The generated Codex hook timeout is 150 seconds, leaving time to return a denial and clean up.

## State and SSH

State defaults to `$XDG_STATE_HOME/herdr/plugins/utahta.codex-confirm`, or `~/.local/state/herdr/plugins/utahta.codex-confirm` when `XDG_STATE_HOME` is unset. Commands are not saved in request files or application logs. To use another state location, add `--state-dir /absolute/path` when generating the hook definition.

Run Codex inside a Herdr pane so the hook can connect to that session. Config and state paths must exist on the machine running Codex.

For SSH, install the plugin on the remote machine, then SSH there and start Herdr's normal UI, or attach with `herdr --remote <ssh-target>`. Run Codex in a remote Herdr pane. The popup and hook communicate on the remote machine; local filesystem paths and GUI forwarding are not used. Single-terminal/direct-agent attachment is not a supported popup path. See [Herdr remote access](https://herdr.dev/docs/persistence-remote/).

## Update and remove

Install the desired published version with `herdr plugin install`, using `--ref` to select a tag:

```sh
herdr plugin install utahta/herdr-codex-confirm --ref v0.1.0
```

After updating, check `plugin_root` with `herdr plugin list`. If the installation path or hook options changed, regenerate the Codex hook definition and review it with `/hooks`. Keep your pattern file outside the installation directory.

To remove the plugin, first remove its `PreToolUse` registration and review the change in Codex. Preserve other hook definitions. Then uninstall it:

```sh
herdr plugin uninstall utahta.codex-confirm
```

Remove your rule file and state only when no longer needed.

## Troubleshooting

| Symptom | What to check |
| --- | --- |
| Plugin installation fails | Check that the repository and requested release are available, and that the installer has its required tools. |
| Codex runs a matching command without a popup | Check detection with `check`, then open `/hooks` to confirm the registration is present, enabled, and trusted. |
| The plugin cannot start | Reinstall the plugin and check that the hook points to `scripts/hook.sh` under the current `plugin_root`. |
| The plugin cannot open a popup | Run Codex inside a Herdr pane with the normal UI attached; check that `utahta.codex-confirm` is enabled. |
| Confirmation happens twice | Check user and project hook sources for duplicate confirmation handlers. |
| Configuration or parse error | Check the indicated regex or shell syntax and ensure `--shell` matches Codex's shell. |
| Unix socket path is too long | Generate the hook with a shorter absolute `--state-dir`. |

## Development

See [development and verification](docs/development.md) for source builds, local Herdr linking, tests, and packaging.

## License

MIT. See [LICENSE](LICENSE).
