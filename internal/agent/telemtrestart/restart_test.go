package telemtrestart

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type recordingRunner struct {
	calls [][]string
	err   error
}

func (r *recordingRunner) Run(_ context.Context, name string, args ...string) error {
	r.calls = append(r.calls, append([]string{name}, args...))
	return r.err
}

func TestParseStrategies(t *testing.T) {
	cases := map[string]struct {
		restartCmd []string
	}{
		"systemd:telemt.service": {
			restartCmd: []string{"systemctl", "restart", "telemt.service"},
		},
		"docker:telemt": {
			restartCmd: []string{"docker", "restart", "telemt"},
		},
	}
	for spec, want := range cases {
		runner := &recordingRunner{}
		r, err := Parse(spec, runner)
		if err != nil {
			t.Fatalf("Parse(%q): %v", spec, err)
		}
		if err := r.Restart(context.Background()); err != nil {
			t.Fatalf("Restart: %v", err)
		}
		if !reflect.DeepEqual(runner.calls[len(runner.calls)-1], want.restartCmd) {
			t.Fatalf("restart cmd = %v, want %v", runner.calls[len(runner.calls)-1], want.restartCmd)
		}
	}
}

func TestParseCommandStrategy(t *testing.T) {
	runner := &recordingRunner{}
	r, err := Parse("command:/usr/local/bin/restart-telemt --now", runner)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := r.Restart(context.Background()); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	want := []string{"/usr/local/bin/restart-telemt", "--now"}
	if !reflect.DeepEqual(runner.calls[0], want) {
		t.Fatalf("cmd = %v, want %v", runner.calls[0], want)
	}
}

func TestParseEmptyOrInvalid(t *testing.T) {
	if _, err := Parse("", &recordingRunner{}); !errors.Is(err, ErrNoStrategy) {
		t.Fatalf("empty: want ErrNoStrategy, got %v", err)
	}
	if _, err := Parse("bogus:x", &recordingRunner{}); err == nil {
		t.Fatalf("bogus: want error, got nil")
	}
	if _, err := Parse("systemd:", &recordingRunner{}); err == nil {
		t.Fatalf("systemd with empty arg: want error, got nil")
	}
}

func TestRestartPropagatesRunnerError(t *testing.T) {
	runner := &recordingRunner{err: errors.New("boom")}
	r, _ := Parse("systemd:telemt.service", runner)
	if err := r.Restart(context.Background()); err == nil {
		t.Fatalf("want error from runner, got nil")
	}
}
