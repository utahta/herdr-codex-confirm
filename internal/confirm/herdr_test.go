package confirm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHerdrCLI(t *testing.T) {
	dir := t.TempDir()
	bin, record := filepath.Join(dir, "herdr"), filepath.Join(dir, "args")
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_SOCKET_PATH", "/tmp/test-session.sock")
	t.Setenv("HERDR_PANE_ID", "workspace:pane")
	t.Setenv("HERDR_BIN_PATH", bin)
	t.Setenv("CONFIRM_TEST_ARGS", record)
	for _, tc := range []struct {
		name, reply string
		fail, busy  bool
	}{
		{"launched", `{"result":{"type":"ok"}}`, false, false},
		{"busy", `{"error":{"code":"ui_busy","message":"busy"}}`, true, true},
		{"failure", `{"error":{"code":"plugin_not_found","message":"not installed"}}`, true, false},
		{"invalid", "not json", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$CONFIRM_TEST_ARGS\"\nprintf '%s\\n' '" + tc.reply + "'"
			if tc.fail {
				script += " >&2\nexit 1"
			}
			if err := os.WriteFile(bin, []byte(script+"\n"), 0755); err != nil {
				t.Fatal(err)
			}
			err := OpenHerdr(context.Background(), "/tmp/request path/s", "unique-id")
			if errors.Is(err, ErrBusy) != tc.busy || (err == nil) != (tc.name == "launched") {
				t.Fatalf("unexpected result: %v", err)
			}
			args, readErr := os.ReadFile(record)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if strings.Contains(string(args), "--target-pane") || !strings.Contains(string(args), "CODEX_CONFIRM_SOCKET=/tmp/request path/s\n") {
				t.Fatalf("invalid popup arguments: %s", args)
			}
		})
	}
}
