package main

import (
	"context"
	"errors"
	"log"
	"time"
)

const (
	// adminOTPDeliveryQueueSize bounds secrets waiting in process memory and keeps
	// a slow provider from turning request goroutines into an unbounded backlog.
	adminOTPDeliveryQueueSize = 16
	// adminOTPDeliveryTimeout bounds one background provider call independently
	// from the browser request that already received its account-neutral result.
	adminOTPDeliveryTimeout = 12 * time.Second
)

var (
	// errAdminOTPDispatcherRequired identifies incomplete production composition.
	errAdminOTPDispatcherRequired = errors.New("administrator OTP dispatcher is required")
	// errAdminOTPDeliveryUnavailable gives known and unknown accounts the same
	// queue-saturation result without exposing whether an email would be sent.
	errAdminOTPDeliveryUnavailable = errors.New("administrator OTP delivery is unavailable")
)

// adminOTPDispatcher accepts both real and account-neutral dummy work. The
// HTTP layer therefore never waits on SMTP or branches its visible timing on
// whether one administrator address exists.
type adminOTPDispatcher interface {
	Dispatch(context.Context, adminOTPDelivery) error
}

// adminOTPDelivery keeps the code and recipient inside one short-lived queue
// item. Deliver=false performs equal queue work without contacting a provider.
type adminOTPDelivery struct {
	Recipient string
	Code      string
	ExpiresAt time.Time
	Deliver   bool
}

// queuedAdminOTPDispatcher owns one bounded worker for the process lifetime.
type queuedAdminOTPDispatcher struct {
	mailer adminOTPMailer
	queue  chan adminOTPDelivery
}

// newQueuedAdminOTPDispatcher starts one worker tied to the server context.
func newQueuedAdminOTPDispatcher(
	ctx context.Context,
	mailer adminOTPMailer,
) (*queuedAdminOTPDispatcher, error) {
	if ctx == nil || mailer == nil {
		return nil, errAdminOTPDispatcherRequired
	}
	dispatcher := &queuedAdminOTPDispatcher{
		mailer: mailer,
		queue:  make(chan adminOTPDelivery, adminOTPDeliveryQueueSize),
	}
	go dispatcher.run(ctx)

	return dispatcher, nil
}

// Dispatch validates even dummy work and never waits for SMTP. A full queue
// fails immediately, allowing the route to return a bounded neutral response.
func (dispatcher *queuedAdminOTPDispatcher) Dispatch(
	ctx context.Context,
	delivery adminOTPDelivery,
) error {
	if dispatcher == nil || dispatcher.queue == nil || ctx == nil ||
		!validAdminOTPCode(delivery.Code) || delivery.ExpiresAt.IsZero() {
		return errAdminOTPDeliveryUnavailable
	}
	if delivery.Deliver && delivery.Recipient == "" {
		return errAdminOTPDeliveryUnavailable
	}

	select {
	case dispatcher.queue <- delivery:
		return nil
	case <-ctx.Done():
		return errAdminOTPDeliveryUnavailable
	default:
		return errAdminOTPDeliveryUnavailable
	}
}

// run drains queued work until server shutdown. Provider errors are recorded
// only as a fixed operational category; recipient, code, and provider text are
// intentionally absent from logs.
func (dispatcher *queuedAdminOTPDispatcher) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case delivery := <-dispatcher.queue:
			if !delivery.Deliver {
				continue
			}
			deliveryContext, cancel := context.WithTimeout(
				ctx,
				adminOTPDeliveryTimeout,
			)
			err := dispatcher.mailer.SendOTP(
				deliveryContext,
				delivery.Recipient,
				delivery.Code,
				delivery.ExpiresAt,
			)
			cancel()
			if err != nil {
				log.Printf(
					"administrator OTP email delivery failed stage=%s",
					adminOTPMailDeliveryStageLabel(err),
				)
			}
		}
	}
}
