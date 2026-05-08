package server

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/lost-coder/panvex/internal/controlplane/webhooks"
)

// publishWebhookEvent is the nil-safe wrapper every event source
// calls. When webhookProducer is unset (no storage configured, e.g.
// in test fixtures), this is a no-op. Errors are logged at warn
// because a webhook outbox failure must NEVER cause the originating
// operation (audit append, agent enroll, …) to fail — the panel's
// durability contract is the audit log, not the webhook delivery.
func (s *Server) publishWebhookEvent(ctx context.Context, action string, payload any) {
	if s == nil || s.webhookProducer == nil {
		return
	}
	body, err := json.Marshal(payload)
	if err != nil {
		s.logger.Warn("webhook publish: marshal payload",
			"action", action,
			"error", err,
		)
		return
	}
	if err := s.webhookProducer.Publish(ctx, webhooks.Event{
		Action:  action,
		Payload: body,
	}); err != nil {
		s.logger.Warn("webhook publish: enqueue",
			"action", action,
			"error", err,
		)
	}
}

// startWebhookWorker spawns the outbox worker goroutine attached to
// the given context (typically rollupCtx, derived from s.serverCtx).
// The worker is a no-op when WebhookStorage was not provided in
// Options — the producer is also nil in that case, so no rows ever
// land in the outbox to begin with.
//
// The worker logs its own errors and survives transient storage
// failures; it returns only when ctx is cancelled.
func (s *Server) startWebhookWorker(ctx context.Context, storage webhooks.Storage) {
	if storage == nil {
		return
	}
	logger := s.logger
	if logger == nil {
		logger = slog.Default()
	}
	worker := webhooks.NewWorker(storage, webhooks.WorkerConfig{
		Logger: logger.With("subsystem", "webhooks"),
		Clock:  s.now,
	})
	go worker.Run(ctx)
}
