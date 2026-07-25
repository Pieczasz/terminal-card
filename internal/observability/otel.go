package observability

import (
	"context"
	"errors"
	"fmt"

	"github.com/Pieczasz/terminal-card/internal/config"

	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

func SetupOTel(ctx context.Context, cfg *config.Config) (shutdown func(context.Context) error, err error) {
	var shutdownFuncs []func(context.Context) error
	shutdown = func(ctx context.Context) error {
		var err error
		for _, fn := range shutdownFuncs {
			err = errors.Join(err, fn(ctx))
		}
		shutdownFuncs = nil
		return err
	}

	res, err := newResource(cfg)
	if err != nil {
		return nil, fmt.Errorf("create resource: %w", err)
	}

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	loggerProvider, err := newLoggerProvider(ctx, cfg, res)
	if err != nil {
		return nil, fmt.Errorf("create logger provider: %w", err)
	}
	shutdownFuncs = append(shutdownFuncs, loggerProvider.Shutdown)
	global.SetLoggerProvider(loggerProvider)

	tracerProvider, err := newTracerProvider(ctx, cfg, res)
	if err != nil {
		return nil, fmt.Errorf("create tracer provider: %w", err)
	}
	shutdownFuncs = append(shutdownFuncs, tracerProvider.Shutdown)
	otel.SetTracerProvider(tracerProvider)

	meterProvider, err := newMeterProvider(ctx, cfg, res)
	if err != nil {
		return nil, fmt.Errorf("create meter provider: %w", err)
	}
	shutdownFuncs = append(shutdownFuncs, meterProvider.Shutdown)
	otel.SetMeterProvider(meterProvider)

	if err := runtime.Start(runtime.WithMeterProvider(meterProvider)); err != nil {
		return nil, fmt.Errorf("start runtime metrics: %w", err)
	}
	if err := registerAppMetrics(meterProvider); err != nil {
		return nil, fmt.Errorf("register app metrics: %w", err)
	}

	return shutdown, nil
}

func newResource(cfg *config.Config) (*resource.Resource, error) {
	version := cfg.ServiceVersion
	if version == "" {
		version = "0.1.0"
	}
	r, err := resource.Merge(resource.Default(),
		resource.NewWithAttributes(resource.Default().SchemaURL(),
			semconv.ServiceName(cfg.Env+"-terminal-card-server"),
			semconv.ServiceVersion(version),
		))
	if err != nil {
		return nil, fmt.Errorf("failed to merge default resource attributes: %w", err)
	}
	return r, nil
}

func newLoggerProvider(ctx context.Context, cfg *config.Config, res *resource.Resource) (*sdklog.LoggerProvider, error) {
	opts := []otlploggrpc.Option{otlploggrpc.WithEndpoint(cfg.OTelEndpoint)}
	if cfg.OTelInsecure {
		opts = append(opts, otlploggrpc.WithInsecure())
	}
	exporter, err := otlploggrpc.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create otlp log exporter: %w", err)
	}
	return sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
		sdklog.WithResource(res),
	), nil
}

func newTracerProvider(ctx context.Context, cfg *config.Config, res *resource.Resource) (*sdktrace.TracerProvider, error) {
	opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(cfg.OTelEndpoint)}
	if cfg.OTelInsecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}
	exporter, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create otlp trace exporter: %w", err)
	}
	return sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	), nil
}

func newMeterProvider(ctx context.Context, cfg *config.Config, res *resource.Resource) (*sdkmetric.MeterProvider, error) {
	opts := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpoint(cfg.OTelEndpoint)}
	if cfg.OTelInsecure {
		opts = append(opts, otlpmetricgrpc.WithInsecure())
	}
	exporter, err := otlpmetricgrpc.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create otlp metric exporter: %w", err)
	}
	return sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter)),
		sdkmetric.WithResource(res),
	), nil
}

// registerAppMetrics exposes the process counters as OTel observable
// instruments, read from their atomics on each collection cycle.
func registerAppMetrics(mp metric.MeterProvider) error {
	meter := mp.Meter("terminal-card")

	active, err := meter.Int64ObservableGauge("terminalcard.ssh.sessions.active",
		metric.WithDescription("Currently connected SSH sessions"))
	if err != nil {
		return fmt.Errorf("create sessions gauge: %w", err)
	}
	started, err := meter.Int64ObservableCounter("terminalcard.games.started",
		metric.WithDescription("Games started since process start"))
	if err != nil {
		return fmt.Errorf("create games counter: %w", err)
	}
	rejects, err := meter.Int64ObservableCounter("terminalcard.ratelimit.rejects",
		metric.WithDescription("Connections rejected by the rate limiter"))
	if err != nil {
		return fmt.Errorf("create rejects counter: %w", err)
	}

	_, err = meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		o.ObserveInt64(active, SSHSessionsActive.Load())
		o.ObserveInt64(started, GamesStartedTotal.Load())
		o.ObserveInt64(rejects, RateLimitRejectsTotal.Load())
		return nil
	}, active, started, rejects)
	if err != nil {
		return fmt.Errorf("register metric callback: %w", err)
	}
	return nil
}
