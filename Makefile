VERSION := $(shell sed -n 's/^version = "\([0-9][0-9.]*\)"$$/\1/p' herdr-plugin.toml)

.PHONY: build test vet popup

build:
	go build -trimpath -ldflags="-s -w -X main.version=$(VERSION)" -o herdr-codex-confirm .

test:
	go test -race ./...

vet:
	go vet ./...

popup: build
	printf '%s\n' '{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"git commit --help"},"cwd":"/tmp"}' | \
		sh scripts/hook.sh --config examples/permissions.json --shell zsh
