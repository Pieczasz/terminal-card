package observability

import (
	"context"
	"errors"
	"fmt"
	"terminalcard/internal/config"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

func SetupOTel(ctx context.Context, cfg *config.Config) (shutdown func(context.Context) error, err error) {
	var shutdownFuncs []func(context.Context) error

	shutdown = func(ctx context.Context) error {
		var err error
		for _, fn := range shutdownFuncs {
			if fnErr := fn(ctx); fnErr != nil {
				err = errors.Join(err, fnErr)
			}
		}
		shutdownFuncs = nil
		return err
	}

	res, err := newResource(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	loggerProvider, err := newLoggerProvider(ctx, cfg, res)
	if err != nil {
		err = errors.Join(err)
		return
	}
	shutdownFuncs = append(shutdownFuncs, loggerProvider.Shutdown)
	global.SetLoggerProvider(loggerProvider)

	return
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
	opts := []otlploggrpc.Option{
		otlploggrpc.WithEndpoint(cfg.OTelEndpoint),
	}
	if cfg.OTelInsecure {
		opts = append(opts, otlploggrpc.WithInsecure())
	}

	logExporter, err := otlploggrpc.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create otlp log exporter: %w", err)
	}

	loggerProvider := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)),
		sdklog.WithResource(res),
	)
	return loggerProvider, nil
}
