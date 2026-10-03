package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRestartWaitsForReplacementAcknowledgement(t *testing.T) {
	t.Setenv(EnvMode, ModeManaged)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- RestartAndWait(ctx) }()
	var request RestartRequest
	select {
	case request = <-RestartRequests:
	case <-ctx.Done():
		t.Fatal("restart request missing")
	}
	select {
	case err := <-finished:
		t.Fatalf("returned before replacement started: %v", err)
	default:
	}
	expected := errors.New("replacement failed")
	request.Done <- expected
	select {
	case err := <-finished:
		if !errors.Is(err, expected) {
			t.Fatalf("got %v", err)
		}
	case <-ctx.Done():
		t.Fatal("acknowledgement not received")
	}
}
func TestRestartWaitCancelsWithoutSupervisor(t *testing.T) {
	t.Setenv(EnvMode, ModeManaged)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := RestartAndWait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}
