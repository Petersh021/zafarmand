package main

import (
	"context"
	"sync"
	"testing"
	"time"
)

// recordingAdminOTPMailer captures only test values after the background worker
// receives them; production addresses and codes never use this implementation.
type recordingAdminOTPMailer struct {
	mu         sync.Mutex
	deliveries []adminOTPDelivery
	notified   chan struct{}
}

func (mailer *recordingAdminOTPMailer) SendOTP(
	_ context.Context,
	recipient string,
	code string,
	expiresAt time.Time,
) error {
	mailer.mu.Lock()
	mailer.deliveries = append(mailer.deliveries, adminOTPDelivery{
		Recipient: recipient,
		Code:      code,
		ExpiresAt: expiresAt,
		Deliver:   true,
	})
	mailer.mu.Unlock()
	select {
	case mailer.notified <- struct{}{}:
	default:
	}

	return nil
}

// TestQueuedAdminOTPDispatcher verifies real delivery and dummy-work privacy.
func TestQueuedAdminOTPDispatcher(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mailer := &recordingAdminOTPMailer{notified: make(chan struct{}, 1)}
	dispatcher, err := newQueuedAdminOTPDispatcher(ctx, mailer)
	if err != nil {
		t.Fatalf("create dispatcher: %v", err)
	}
	expiresAt := time.Now().UTC().Add(5 * time.Minute)
	if err := dispatcher.Dispatch(ctx, adminOTPDelivery{
		Recipient: "owner@example.test",
		Code:      "4719",
		ExpiresAt: expiresAt,
		Deliver:   true,
	}); err != nil {
		t.Fatalf("dispatch real delivery: %v", err)
	}
	select {
	case <-mailer.notified:
	case <-time.After(time.Second):
		t.Fatal("worker did not deliver queued OTP")
	}

	if err := dispatcher.Dispatch(ctx, adminOTPDelivery{
		Code:      "0007",
		ExpiresAt: expiresAt,
		Deliver:   false,
	}); err != nil {
		t.Fatalf("dispatch dummy work: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	mailer.mu.Lock()
	defer mailer.mu.Unlock()
	if len(mailer.deliveries) != 1 {
		t.Errorf("provider deliveries: got %d, want 1", len(mailer.deliveries))
	}
}

// TestQueuedAdminOTPDispatcherRejectsMalformedWork checks the fail-closed edge.
func TestQueuedAdminOTPDispatcherRejectsMalformedWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dispatcher := &queuedAdminOTPDispatcher{
		queue: make(chan adminOTPDelivery, 1),
	}
	if err := dispatcher.Dispatch(ctx, adminOTPDelivery{}); err != errAdminOTPDeliveryUnavailable {
		t.Errorf("malformed work: got %v", err)
	}
}
