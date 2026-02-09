package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDoRetries(t *testing.T) {
	attempts := 0
	err := Do(context.Background(), 3, 1*time.Millisecond, func() error {
		attempts++
		if attempts < 3 {
			return errors.New("fail")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestDoContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Do(ctx, 3, 1*time.Millisecond, func() error { return errors.New("fail") })
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}
