package main

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

func TestStopChildWaitsForExit(t *testing.T) {
	child := exec.Command("sleep", "30")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer child.Process.Kill()
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	stopChild(child, done)
	if child.ProcessState == nil {
		t.Fatal("child exit has not been reaped")
	}
}

func TestRetryStopsWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if waitForRetry(ctx, time.Minute) {
		t.Fatal("retry continued after cancellation")
	}
}
