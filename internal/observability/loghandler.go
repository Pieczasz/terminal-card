package observability

import (
	"context"
	"errors"
	"log/slog"
)

// fanoutHandler dispatches each record to every wrapped handler, so logs can
// reach stderr (always visible to the container runtime) and an exporter at the
// same time. A failure in one sink never hides the others.
type fanoutHandler struct {
	handlers []slog.Handler
}

// NewFanoutHandler returns a slog.Handler that writes to all handlers.
func NewFanoutHandler(handlers ...slog.Handler) slog.Handler {
	return fanoutHandler{handlers: handlers}
}

func (h fanoutHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, sub := range h.handlers {
		if sub.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h fanoutHandler) Handle(ctx context.Context, record slog.Record) error {
	var errs []error
	for _, sub := range h.handlers {
		if sub.Enabled(ctx, record.Level) {
			errs = append(errs, sub.Handle(ctx, record.Clone()))
		}
	}
	return errors.Join(errs...)
}

func (h fanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	subs := make([]slog.Handler, len(h.handlers))
	for i, sub := range h.handlers {
		subs[i] = sub.WithAttrs(attrs)
	}
	return fanoutHandler{handlers: subs}
}

func (h fanoutHandler) WithGroup(name string) slog.Handler {
	subs := make([]slog.Handler, len(h.handlers))
	for i, sub := range h.handlers {
		subs[i] = sub.WithGroup(name)
	}
	return fanoutHandler{handlers: subs}
}
