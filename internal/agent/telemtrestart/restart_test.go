package telemtrestart

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/lost-coder/panvex/internal/restartspec"
)

type fakeRunner struct {
	calls [][]string
	err   error
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) error {
	f.calls = append(f.calls, append([]string{name}, args...))
	return f.err
}

func TestNewAndRestart(t *testing.T) {
	fake := &fakeRunner{}
	r, err := New("systemd:telemt", fake)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := r.Restart(context.Background()); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	want := []string{"systemctl", "restart", "telemt"}
	if len(fake.calls) != 1 || !reflect.DeepEqual(fake.calls[0], want) {
		t.Fatalf("calls = %v, want [%v]", fake.calls, want)
	}
}

func TestNewEmptySpecIsRestartspecErrEmpty(t *testing.T) {
	if _, err := New("", &fakeRunner{}); !errors.Is(err, restartspec.ErrEmpty) {
		t.Fatalf("want restartspec.ErrEmpty, got %v", err)
	}
}

func TestRestartPropagatesRunnerError(t *testing.T) {
	fake := &fakeRunner{err: errors.New("boom")}
	r, err := New("systemd:telemt", fake)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = r.Restart(context.Background())
	if err == nil {
		t.Fatalf("want error from runner, got nil")
	}
	if !strings.Contains(err.Error(), "systemctl restart telemt") {
		t.Fatalf("error %q should contain the command text", err.Error())
	}
	if !errors.Is(err, fake.err) {
		t.Fatalf("want wrapped runner error, got %v", err)
	}
}
