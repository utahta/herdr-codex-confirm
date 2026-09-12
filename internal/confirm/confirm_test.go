package confirm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func stateDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "confirm-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func assertClean(t *testing.T, state string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(state, "requests"))
	if err != nil || len(entries) != 0 {
		t.Errorf("request state not cleaned: %v %v", entries, err)
	}
}

func TestRoundTrip(t *testing.T) {
	for _, decision := range []string{"approve", "deny", "disconnect", "wrong_id", "invalid", "timeout", "error"} {
		t.Run(decision, func(t *testing.T) {
			state := stateDir(t)
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			done := make(chan error, 1)
			err := Ask(ctx, state, Request{Command: "git push", Cwd: "/tmp/repo", Rules: [][]string{{"git", "push"}}}, func(_ context.Context, socket, id string) error {
				go func() {
					conn, err := net.Dial("unix", socket)
					if err != nil {
						done <- err
						return
					}
					defer conn.Close()
					conn.SetDeadline(time.Now().Add(time.Second))
					enc, dec := json.NewEncoder(conn), json.NewDecoder(conn)
					if err := enc.Encode(Response{ID: id, Decision: "connect"}); err != nil {
						done <- err
						return
					}
					var req Request
					if err := dec.Decode(&req); err != nil {
						done <- err
						return
					}
					if req.ID != id || req.Command != "git push" || req.Cwd != "/tmp/repo" || !reflect.DeepEqual(req.Rules, [][]string{{"git", "push"}}) {
						done <- errors.New("request data changed")
						return
					}
					switch decision {
					case "disconnect":
					case "timeout":
						<-ctx.Done()
						enc.Encode(Response{ID: id, Decision: "approve"})
					case "wrong_id":
						enc.Encode(Response{ID: "other-request", Decision: "approve"})
					default:
						enc.Encode(Response{ID: id, Decision: decision})
					}
					done <- nil
				}()
				return nil
			})
			if (err == nil) != (decision == "approve") {
				t.Fatalf("%s returned %v", decision, err)
			}
			if clientErr := <-done; clientErr != nil {
				t.Fatal(clientErr)
			}
			assertClean(t, state)
		})
	}
}

func TestBusyAndLaunchFailure(t *testing.T) {
	for _, tc := range []struct {
		name    string
		failure error
		retries int
	}{
		{"launch failure", errors.New("plugin missing"), 1},
		{"busy timeout", ErrBusy, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := stateDir(t)
			ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
			defer cancel()
			calls := 0
			err := Ask(ctx, state, Request{}, func(context.Context, string, string) error { calls++; return tc.failure })
			if err == nil || calls != tc.retries {
				t.Fatalf("error=%v calls=%d", err, calls)
			}
			assertClean(t, state)
		})
	}
}

func TestConcurrentRequestsAndPopup(t *testing.T) {
	state := stateDir(t)
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	var seen sync.Map
	for i := range 8 {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			done := make(chan error, 1)
			command := fmt.Sprintf("echo request-%d", i)
			pane := fmt.Sprintf("session-%d:pane", i)
			approved := i%2 == 0
			err := Ask(ctx, state, Request{Command: command, Cwd: "/tmp", SourcePane: pane}, func(_ context.Context, socket, id string) error {
				if _, loaded := seen.LoadOrStore(id, true); loaded {
					return errors.New("reused request ID")
				}
				go func() {
					done <- Popup(ctx, socket, id, func(_ context.Context, req Request) (Result, error) {
						if req.Command != command || req.SourcePane != pane {
							return Result{}, errors.New("received another request's data")
						}
						return Result{Approved: approved, Feedback: command}, nil
					})
				}()
				return nil
			})
			if (err == nil) != approved {
				errs <- fmt.Errorf("request %d: approval=%t err=%v", i, approved, err)
			} else if !approved && !strings.HasSuffix(err.Error(), "User feedback:\n"+command) {
				errs <- fmt.Errorf("request %d: feedback changed: %v", i, err)
			} else {
				errs <- nil
			}
			errs <- <-done
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	assertClean(t, state)
}

func TestBusyThenAnswer(t *testing.T) {
	state := stateDir(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	calls := 0
	err := Ask(ctx, state, Request{}, func(_ context.Context, socket, id string) error {
		calls++
		if calls == 1 {
			return ErrBusy
		}
		go func() {
			done <- Popup(ctx, socket, id, func(context.Context, Request) (Result, error) { return Result{Approved: true}, nil })
		}()
		return nil
	})
	if err != nil || calls != 2 {
		t.Fatalf("error=%v calls=%d", err, calls)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	assertClean(t, state)
}

func TestCancellationClosesPopup(t *testing.T) {
	state := stateDir(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	stopped := make(chan struct{})
	done := make(chan struct{})
	err := Ask(ctx, state, Request{}, func(_ context.Context, socket, id string) error {
		go func() {
			defer close(done)
			Popup(context.Background(), socket, id, func(uiCtx context.Context, _ Request) (Result, error) {
				cancel()
				<-uiCtx.Done()
				close(stopped)
				return Result{}, uiCtx.Err()
			})
		}()
		return nil
	})
	if err == nil {
		t.Fatal("approved canceled hook")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("popup survived hook disconnect")
	}
	<-done
	assertClean(t, state)
}

func TestPrivateDirectoryAndStaleSocket(t *testing.T) {
	state := stateDir(t)
	private := filepath.Join(state, "requests")
	os.Mkdir(private, 0755)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := Ask(ctx, state, Request{}, func(context.Context, string, string) error { return nil }); err == nil || !strings.Contains(err.Error(), "0700") {
		t.Fatalf("unsafe directory accepted: %v", err)
	}
	os.Chmod(private, 0700)
	var stale string
	Ask(ctx, state, Request{}, func(_ context.Context, socket, _ string) error { stale = socket; return errors.New("failed") })
	if c, err := net.Dial("unix", stale); err == nil {
		c.Close()
		t.Fatal("stale request is still listening")
	}
	assertClean(t, state)
}
