package observability

import (
	"context"
	"net"
	"testing"

	"github.com/Pieczasz/terminal-card/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/global"
	collogpb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetricpb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type mockLogsService struct {
	collogpb.UnimplementedLogsServiceServer
	requests []*collogpb.ExportLogsServiceRequest
}

func (m *mockLogsService) Export(ctx context.Context, req *collogpb.ExportLogsServiceRequest) (*collogpb.ExportLogsServiceResponse, error) {
	m.requests = append(m.requests, req)
	return &collogpb.ExportLogsServiceResponse{}, nil
}

// SetupOTel now also exports traces and metrics; the mock endpoint must accept
// them so shutdown flushes cleanly.
type mockTraceService struct {
	coltracepb.UnimplementedTraceServiceServer
}

func (mockTraceService) Export(context.Context, *coltracepb.ExportTraceServiceRequest) (*coltracepb.ExportTraceServiceResponse, error) {
	return &coltracepb.ExportTraceServiceResponse{}, nil
}

type mockMetricsService struct {
	colmetricpb.UnimplementedMetricsServiceServer
}

func (mockMetricsService) Export(context.Context, *colmetricpb.ExportMetricsServiceRequest) (*colmetricpb.ExportMetricsServiceResponse, error) {
	return &colmetricpb.ExportMetricsServiceResponse{}, nil
}

func TestOTel_Integration(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	grpcServer := grpc.NewServer(grpc.Creds(insecure.NewCredentials()))
	mockSvc := &mockLogsService{}
	collogpb.RegisterLogsServiceServer(grpcServer, mockSvc)
	coltracepb.RegisterTraceServiceServer(grpcServer, mockTraceService{})
	colmetricpb.RegisterMetricsServiceServer(grpcServer, mockMetricsService{})

	go func() {
		_ = grpcServer.Serve(lis)
	}()
	defer grpcServer.Stop()

	addr := lis.Addr().String()
	cfg := &config.Config{
		OTelEndpoint: addr,
		OTelInsecure: true,
		Env:          "development",
	}

	ctx := context.Background()
	shutdown, err := SetupOTel(ctx, cfg)
	require.NoError(t, err)

	logger := global.Logger("test-logger")

	var record otellog.Record
	record.SetBody(otellog.StringValue("hello from test"))

	logger.Emit(ctx, record)

	err = shutdown(ctx)
	require.NoError(t, err)

	require.NotEmpty(t, mockSvc.requests, "expected mock server to receive logs")
	req := mockSvc.requests[0]

	require.NotEmpty(t, req.ResourceLogs)
	assert.Equal(t, "development-terminal-card-server", req.ResourceLogs[0].Resource.Attributes[0].Value.GetStringValue())
}
