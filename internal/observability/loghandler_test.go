package observability

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type captureHandler struct {
	level    slog.Level
	records  []slog.Record
	attrs    []slog.Attr
	groups   []string
	failWith error
}

func (h *captureHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *captureHandler) Handle(_ context.Context, record slog.Record) error {
	h.records = append(h.records, record)
	return h.failWith
}

func (h *captureHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &captureHandler{level: h.level, attrs: append(h.attrs, attrs...), groups: h.groups, failWith: h.failWith}
}

func (h *captureHandler) WithGroup(name string) slog.Handler {
	return &captureHandler{level: h.level, attrs: h.attrs, groups: append(h.groups, name), failWith: h.failWith}
}

func newRecord(level slog.Level, msg string) slog.Record {
	return slog.NewRecord(testTime(), level, msg, 0)
}

func TestFanoutHandler_Handle_ReachesEverySink(t *testing.T) {
	t.Parallel()
	a := &captureHandler{level: slog.LevelInfo}
	b := &captureHandler{level: slog.LevelInfo}

	require.NoError(t, NewFanoutHandler(a, b).Handle(context.Background(), newRecord(slog.LevelInfo, "hello")))

	require.Len(t, a.records, 1)
	require.Len(t, b.records, 1)
	assert.Equal(t, "hello", a.records[0].Message)
	assert.Equal(t, "hello", b.records[0].Message)
}

func TestFanoutHandler_Handle_OneFailingSinkDoesNotHideTheOthers(t *testing.T) {
	t.Parallel()
	boom := errors.New("exporter unavailable")
	failing := &captureHandler{level: slog.LevelInfo, failWith: boom}
	healthy := &captureHandler{level: slog.LevelInfo}

	err := NewFanoutHandler(failing, healthy).Handle(context.Background(), newRecord(slog.LevelError, "db down"))

	require.ErrorIs(t, err, boom, "the failure must be reported, not swallowed")
	require.Len(t, healthy.records, 1, "the healthy sink still received the record")
	assert.Equal(t, "db down", healthy.records[0].Message)
}

func TestFanoutHandler_Handle_SkipsSinksThatAreNotEnabled(t *testing.T) {
	t.Parallel()
	quiet := &captureHandler{level: slog.LevelError}
	chatty := &captureHandler{level: slog.LevelDebug}

	require.NoError(t, NewFanoutHandler(quiet, chatty).Handle(context.Background(), newRecord(slog.LevelInfo, "info only")))

	assert.Empty(t, quiet.records, "a sink above the record's level is skipped")
	assert.Len(t, chatty.records, 1)
}

func TestFanoutHandler_Enabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		levels []slog.Level
		ask    slog.Level
		want   bool
	}{
		{name: "any sink enabled wins", levels: []slog.Level{slog.LevelError, slog.LevelDebug}, ask: slog.LevelInfo, want: true},
		{name: "no sink enabled", levels: []slog.Level{slog.LevelError, slog.LevelError}, ask: slog.LevelInfo, want: false},
		{name: "exact level is enabled", levels: []slog.Level{slog.LevelInfo}, ask: slog.LevelInfo, want: true},
		{name: "no sinks at all", levels: nil, ask: slog.LevelInfo, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			subs := make([]slog.Handler, 0, len(tt.levels))
			for _, level := range tt.levels {
				subs = append(subs, &captureHandler{level: level})
			}
			assert.Equal(t, tt.want, NewFanoutHandler(subs...).Enabled(context.Background(), tt.ask))
		})
	}
}

func TestFanoutHandler_WithAttrsAndGroupApplyToEverySink(t *testing.T) {
	t.Parallel()
	a := &captureHandler{level: slog.LevelInfo}
	b := &captureHandler{level: slog.LevelInfo}

	derived := NewFanoutHandler(a, b).
		WithAttrs([]slog.Attr{slog.String("service", "terminal-card")}).
		WithGroup("request")

	fanout, ok := derived.(fanoutHandler)
	require.True(t, ok)
	require.Len(t, fanout.handlers, 2)

	for _, sub := range fanout.handlers {
		capture, ok := sub.(*captureHandler)
		require.True(t, ok)
		assert.Equal(t, []string{"request"}, capture.groups)
		require.Len(t, capture.attrs, 1)
		assert.Equal(t, "service", capture.attrs[0].Key)
	}
}

type discardHandler struct{}

func (discardHandler) Enabled(context.Context, slog.Level) bool  { return true }
func (discardHandler) Handle(context.Context, slog.Record) error { return nil }
func (h discardHandler) WithAttrs([]slog.Attr) slog.Handler      { return h }
func (h discardHandler) WithGroup(string) slog.Handler           { return h }

func BenchmarkFanoutHandler_Handle(b *testing.B) {
	handler := NewFanoutHandler(discardHandler{}, discardHandler{})
	record := newRecord(slog.LevelInfo, "player joined lobby")
	record.AddAttrs(slog.String("player", "alice"), slog.Int("seat", 3))
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		_ = handler.Handle(ctx, record)
	}
}

func testTime() time.Time {
	return time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
}
