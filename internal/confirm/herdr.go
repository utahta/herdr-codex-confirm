package confirm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"
)

func OpenHerdr(ctx context.Context, socket, id string) error {
	if os.Getenv("HERDR_ENV") != "1" || os.Getenv("HERDR_SOCKET_PATH") == "" || os.Getenv("HERDR_PANE_ID") == "" {
		return fmt.Errorf("run Codex inside a Herdr pane with the normal session UI attached")
	}
	bin := os.Getenv("HERDR_BIN_PATH")
	if bin == "" {
		bin = "herdr"
	}
	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(callCtx, bin, "plugin", "pane", "open", "--plugin", PluginID, "--entrypoint", "confirm",
		"--env", "CODEX_CONFIRM_SOCKET="+socket, "--env", "CODEX_CONFIRM_ID="+id)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = 250 * time.Millisecond
	err := cmd.Run()
	for _, data := range [][]byte{stderr.Bytes(), stdout.Bytes()} {
		var envelope struct {
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(data, &envelope) != nil {
			continue
		}
		if envelope.Error != nil {
			if envelope.Error.Code == "ui_busy" {
				return ErrBusy
			}
			return fmt.Errorf("Herdr rejected popup launch: %s (%s)", envelope.Error.Message, envelope.Error.Code)
		}
		if err == nil && len(envelope.Result) > 0 && string(envelope.Result) != "null" {
			return nil
		}
	}
	if err != nil {
		return fmt.Errorf("Herdr CLI failed: %w", err)
	}
	return fmt.Errorf("Herdr returned an invalid popup launch result")
}
