package confirm

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const PluginID = "utahta.codex-confirm"
const MaxMessage = 2 << 20

var ErrBusy = errors.New("Herdr UI is busy")

type Request struct {
	ID         string     `json:"id"`
	Command    string     `json:"command"`
	Cwd        string     `json:"cwd"`
	SourcePane string     `json:"source_pane,omitempty"`
	Rules      [][]string `json:"rules"`
	Deadline   time.Time  `json:"deadline"`
}

type Response struct {
	ID       string `json:"id"`
	Decision string `json:"decision"`
	Feedback string `json:"feedback,omitempty"`
}

type Result struct {
	Approved bool
	Feedback string
}

type Open func(context.Context, string, string) error

func StateDir() (string, error) {
	if dir := os.Getenv("HERDR_PLUGIN_STATE_DIR"); dir != "" && os.Getenv("HERDR_PLUGIN_ID") == PluginID {
		return dir, nil
	}
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "state")
	}
	if !filepath.IsAbs(base) {
		return "", fmt.Errorf("XDG_STATE_HOME must be absolute")
	}
	return filepath.Join(base, "herdr", "plugins", PluginID), nil
}

func Ask(ctx context.Context, state string, req Request, open Open) error {
	if !filepath.IsAbs(state) {
		return fmt.Errorf("state directory must be absolute")
	}
	private := filepath.Join(state, "requests")
	if err := os.MkdirAll(private, 0700); err != nil {
		return fmt.Errorf("create request directory: %w", err)
	}
	info, err := os.Lstat(private)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
		return fmt.Errorf("request directory must be owned by the current user with mode 0700")
	}
	dir, err := os.MkdirTemp(private, "r-")
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "s")
	if len(socket) >= 104 {
		return fmt.Errorf("request socket path is too long; use a shorter --state-dir")
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		return fmt.Errorf("create request socket: %w", err)
	}
	defer listener.Close()
	stop := context.AfterFunc(ctx, func() { listener.Close() })
	defer stop()
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return fmt.Errorf("create request ID: %w", err)
	}
	req.ID = hex.EncodeToString(token)
	deadline, ok := ctx.Deadline()
	if !ok {
		return fmt.Errorf("confirmation requires a deadline")
	}
	req.Deadline = deadline
	for {
		if ctx.Err() != nil {
			return fmt.Errorf("confirmation timed out or was interrupted while waiting for Herdr")
		}
		err = open(ctx, socket, req.ID)
		if !errors.Is(err, ErrBusy) {
			break
		}
		select {
		case <-ctx.Done():
		case <-time.After(250 * time.Millisecond):
		}
	}
	if err != nil {
		return fmt.Errorf("open confirmation popup: %w", err)
	}
	listener.SetDeadline(minTime(deadline, time.Now().Add(5*time.Second)))
	conn, err := listener.AcceptUnix()
	if err != nil {
		return fmt.Errorf("confirmation popup did not connect before the deadline")
	}
	defer conn.Close()
	stopConn := context.AfterFunc(ctx, func() { conn.Close() })
	defer stopConn()
	conn.SetDeadline(minTime(deadline, time.Now().Add(5*time.Second)))
	dec := json.NewDecoder(io.LimitReader(conn, MaxMessage))
	var hello Response
	if err := dec.Decode(&hello); err != nil || hello.ID != req.ID || hello.Decision != "connect" {
		return fmt.Errorf("invalid confirmation popup handshake")
	}
	conn.SetDeadline(deadline)
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return fmt.Errorf("confirmation popup disconnected")
	}
	var answer Response
	if err := dec.Decode(&answer); err != nil {
		if ctx.Err() != nil || !time.Now().Before(deadline) {
			return fmt.Errorf("confirmation timed out or was interrupted")
		}
		return fmt.Errorf("confirmation popup disconnected before answering")
	}
	if ctx.Err() != nil || !time.Now().Before(deadline) {
		return fmt.Errorf("confirmation timed out or was interrupted")
	}
	if answer.ID != req.ID {
		return fmt.Errorf("confirmation response belongs to a different request")
	}
	switch answer.Decision {
	case "approve":
		return nil
	case "deny":
		if feedback := strings.TrimSpace(answer.Feedback); feedback != "" {
			return fmt.Errorf("command was denied by the user.\nUser feedback:\n%s", feedback)
		}
		return fmt.Errorf("command was denied by the user")
	case "timeout":
		return fmt.Errorf("confirmation timed out")
	case "error":
		return fmt.Errorf("confirmation UI failed")
	default:
		return fmt.Errorf("invalid confirmation response")
	}
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func Popup(ctx context.Context, socket, id string, show func(context.Context, Request) (Result, error)) error {
	if socket == "" || id == "" {
		return fmt.Errorf("popup must be opened by a pending confirmation hook")
	}
	dialCtx, stopDial := context.WithTimeout(ctx, 5*time.Second)
	defer stopDial()
	conn, err := (&net.Dialer{}).DialContext(dialCtx, "unix", socket)
	if err != nil {
		return fmt.Errorf("connect to confirmation hook: %w", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	enc := json.NewEncoder(conn)
	if err := enc.Encode(Response{ID: id, Decision: "connect"}); err != nil {
		return err
	}
	var req Request
	dec := json.NewDecoder(io.LimitReader(conn, MaxMessage))
	if err := dec.Decode(&req); err != nil {
		return fmt.Errorf("read confirmation request: %w", err)
	}
	if req.ID != id || req.Deadline.IsZero() || !time.Now().Before(req.Deadline) {
		return fmt.Errorf("invalid or expired confirmation request")
	}
	conn.SetDeadline(req.Deadline)
	uiCtx, cancel := context.WithDeadline(ctx, req.Deadline)
	defer cancel()
	// Closing the hook connection also closes an unanswered popup.
	go func() { io.Copy(io.Discard, conn); cancel() }()
	result, uiErr := show(uiCtx, req)
	answer := Response{ID: id, Decision: "deny"}
	if !time.Now().Before(req.Deadline) {
		answer.Decision = "timeout"
	} else if uiErr != nil || uiCtx.Err() != nil {
		answer.Decision = "error"
	} else if result.Approved {
		answer.Decision = "approve"
	} else {
		answer.Feedback = strings.TrimSpace(result.Feedback)
	}
	if err := enc.Encode(answer); err != nil {
		return fmt.Errorf("return confirmation response: %w", err)
	}
	return uiErr
}
