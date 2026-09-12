# Development and verification

## Local installation

Use a local checkout when developing the plugin. This requires Git, Go 1.26 or later, and Make. Run these commands in a Herdr terminal and keep the checkout in a stable location:

```sh
git clone https://github.com/utahta/herdr-codex-confirm.git
cd herdr-codex-confirm
make build
herdr plugin link .
```

You can use `sh install.sh` instead of `make build` if Make is unavailable.

The `.` passed to `plugin link` is the directory containing `herdr-plugin.toml`. `./herdr-codex-confirm` is the executable; linking that file produces an invalid UTF-8 error. Linking does not build the executable.

Continue with [the pattern and Codex hook setup](../README.md#2-choose-which-commands-need-confirmation), using this checkout as the installation directory. Herdr linking does not register the Codex hook.

After changing the source, rerun `make build`. If you move the checkout, run `herdr plugin link .` in the new location, regenerate the Codex hook definition, and review it with `/hooks`. To remove a local registration, first remove its Codex hook entry, then run `herdr plugin unlink utahta.codex-confirm`.

## Build and test

`make` or `make build` builds `./herdr-codex-confirm` with the version from `herdr-plugin.toml`. `make test` runs all Go tests with the race detector, and `make vet` runs static checks. These targets require Go and Make.

```sh
make build
make test
make vet
for script in install.sh scripts/*.sh; do sh -n "$script"; done
sh scripts/release.sh
sh scripts/install_test.sh
```

The last two commands also require GoReleaser v2 and ssh-keygen with `-Y sign` and `-Y verify` support. They build a signed local snapshot and test the installer without publishing anything.

Tests cover command detection, hook and popup decisions, and feedback input. Approval tests use stub responses and never run commit, push, or GitHub writes.

## Popup check

After completing [local installation](#local-installation), run this from a Herdr terminal in the checkout to check the popup independently of Codex:

```sh
make popup
```

The target builds the binary, then feeds a test hook request to `scripts/hook.sh` using `examples/permissions.json`. It displays `git commit --help` with `/tmp` as the working directory. The Git command is never executed, even if approved.

The manifest requests a 90-column by 18-row popup, including its border; Herdr limits it to the available terminal area. Short content keeps the buttons close to the command. Longer content scrolls while the action buttons and countdown remain visible.

The popup starts with **Deny** selected. Tab twice then Enter approves and returns `{}`; Escape returns a JSON denial. Tab once then Enter opens **Deny with feedback**. Enter inserts a newline in the editor; Ctrl+S denies and includes the submitted text in `permissionDecisionReason`. Escape returns to the action selection with the draft retained, and Ctrl+C denies without sending it. This confirms that the plugin can open a popup and return an answer. It does not establish that Codex invokes the hook; use the integration check below for that path.

## Codex integration check

Create a temporary rule file containing `{"patterns":[["printf","codex-confirm-probe"]]}`. Generate and register a hook for it with `hook-config`, replacing the existing confirmation entry, and review and trust the definition with `/hooks`.

Ask Codex to run `printf codex-confirm-probe` through its shell tool. Check that it waits for the popup, denial produces no probe output, and approval prints the marker exactly once. Choose **Deny with feedback** and send "Use printf other-probe instead"; check that the original command stays blocked and that Codex receives the instruction. Check that `printf other-probe` needs no popup. Restore the intended rule file and hook registration afterwards, and review the restored definition in `/hooks`.

Repeat approval, denial, feedback input, Escape, scrolling, and expiry on the target Linux/SSH UI before declaring those paths verified. Directly feeding JSON to `scripts/hook.sh` tests the plugin and UI, but does not establish that Codex invokes or enforces the hook.

## Verified environments

Interactive verification is currently limited to macOS arm64. End-to-end execution through a trusted Codex hook and Linux/SSH popup behavior remain unverified. Use the [Codex integration check](#codex-integration-check) to verify those paths.

## Packaging

`.goreleaser.yaml` defines macOS/Linux archives for amd64 and arm64. Each archive contains the binary, this project's `LICENSE`, and `THIRD_PARTY_NOTICES`. The `scripts/licenses.sh` hook downloads dependency sources before collecting their license texts. Checksums are written to `checksums.txt`, and an Ed25519 OpenSSH signature is written to `checksums.txt.sig` using the `herdr-codex-confirm-release` namespace.

`sh scripts/release.sh` runs GoReleaser in snapshot mode with the manifest version, preserving the asset names expected by the installer. It generates a temporary test signing key, leaves its public verification file at `dist/signing-key.pub`, and removes the private key when it exits. These snapshots are for testing; their signatures do not match the production public key. The script cleans `dist/` and never publishes.

Packaging CI runs on macOS and Linux. `scripts/install_test.sh` checks all four archives and uses local fixtures to test installation and rejection of invalid signatures, checksums, and archives.

See the README for [installation requirements](../README.md#requirements) and [setup instructions](../README.md#setup).

## Release automation

On pushes to `main`, tagpr creates or updates a release pull request containing the version change in `herdr-plugin.toml` and `CHANGELOG.md`. Merging that pull request creates the version tag. The same workflow then checks out that tag, checks it against the manifest, runs tests, validates the signing key, and publishes with GoReleaser. Tags created with `GITHUB_TOKEN` do not start a separate push workflow. See [tagpr's release flow](https://github.com/Songmu/tagpr#tagging-and-release).

Enable **Settings > Actions > General > Workflow permissions > Allow GitHub Actions to create and approve pull requests**. The workflow requests `contents: write`, `pull-requests: write`, and `issues: read`. Workflows on tagpr's release pull requests may require approval through the PR's **Approve workflows to run** control. See [GitHub's workflow triggering rules](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/trigger-a-workflow).

Configure the repository's Actions secret `RELEASE_SIGNING_KEY` with the unencrypted Ed25519 private key corresponding to `release/signing-key.pub`. The public file uses OpenSSH allowed-signers format: `herdr-codex-confirm-release ssh-ed25519 <public-key>`. The private key must be stored outside the checkout. To provision a new key before releasing, choose a durable private directory and run:

```sh
signing_dir=/absolute/path/to/private-key-directory
mkdir -p "$signing_dir"
chmod 700 "$signing_dir"
ssh-keygen -t ed25519 -N '' -C herdr-codex-confirm-release -f "$signing_dir/signing-key"
printf 'herdr-codex-confirm-release %s\n' "$(cat "$signing_dir/signing-key.pub")" > release/signing-key.pub
gh secret set RELEASE_SIGNING_KEY --repo utahta/herdr-codex-confirm < "$signing_dir/signing-key"
```

Review the public-key change before merging it. The release workflow signs a probe and verifies it against the committed public key before building a release, so a missing or mismatched secret stops publication. Ordinary CI uses temporary test keys and needs no production secret.

GoReleaser uploads all assets to a draft before publishing. If a run fails after the tag exists, use **Actions > tagpr > Run workflow** on `main` and enter that existing tag, for example `v0.1.0`. This checks out `refs/tags/v0.1.0` and runs only the publication steps. GoReleaser reuses an unfinished draft and replaces partial uploads. Re-running tagpr on an ordinary push is insufficient because its tag output is only populated when it creates a tag. Published releases should be fixed with a new version.

## Hook and popup communication

The executable and repository are named `herdr-codex-confirm`. The Herdr plugin ID is `utahta.codex-confirm`. The generated hook invokes `scripts/hook.sh`, which runs the binary's `hook` subcommand and converts unsuccessful exits to exit code 2. The generated command also ends with `|| exit 2` so a missing wrapper script blocks execution. Approval returns `{}`; denial or failure returns a reason with `permissionDecision: "deny"`. The hook never executes the submitted command.

The hook inherits `HERDR_ENV`, `HERDR_SOCKET_PATH`, `HERDR_PANE_ID`, and, when available, `HERDR_BIN_PATH` from its Herdr pane. It connects to that session and uses `herdr` on PATH when the binary path is absent. State uses `HERDR_PLUGIN_STATE_DIR` when `HERDR_PLUGIN_ID` matches `utahta.codex-confirm`; otherwise it follows the [default state paths](../README.md#state-and-ssh).

Each request uses its own private directory, Unix socket, and random ID. Commands travel over the socket after an ID handshake and are not saved in request files or application logs. Answers must belong to the same connection and request. Completed requests remove their directory and socket. An abruptly killed process may leave an inactive directory, which cannot approve a future request.

The popup response includes an optional `feedback` field for explicit denial. The hook returns it as user feedback in `permissionDecisionReason`, preserving line breaks and punctuation after trimming surrounding whitespace. Approval, UI failure, expiry, and disconnection do not forward drafts. Feedback travels over the same socket as the decision and is not saved by the plugin.

Concurrent requests have distinct sockets and IDs, even across sessions. When Herdr returns `ui_busy`, the hook retries every 250 ms within the total deadline. Ordering is not FIFO. It never closes another popup. Herdr CLI calls and the initial popup connection have 5-second limits. Herdr session modals do not accept a target pane; the request separately carries the source pane for display.

The state directory's `requests` child must be owned by the current user with mode 0700. Use a shorter state path if a Unix socket pathname would reach 104 bytes. Hook JSON input is limited to 1 MiB, independently of the scrollable UI.

## Command analysis

The detector uses [`mvdan.cc/sh/v3` v3.14.1](https://pkg.go.dev/mvdan.cc/sh/v3@v3.14.1/syntax#LangVariant). Its zsh support is experimental and incomplete. The [detection scope](../README.md#detection-scope) describes the user-visible matching behavior and exclusions.

The normalized hook input does not reliably identify the original shell, so `--shell` must agree with the shell Codex executes. Explicit shell `-c` strings are parsed using their own shell variant. The detector performs static analysis and does not source shell startup files or execute substitutions.

Literal shell `-c` strings are analyzed recursively, up to 32 levels. Each shell argument can produce at most 16,384 brace-expansion words. Exceeding either limit returns an analysis error, which causes the hook to deny the tool call.
