package driver

import (
	"context"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// csiCalls and csiCallDuration count and time every CSI call the sidecars
// and the kubelet make, by operation and by the gRPC code it returned.
var (
	csiCalls = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "synology_csi_calls_total",
		Help: "CSI calls the driver answered, by operation and gRPC code.",
	}, []string{"operation", "code"})
	csiCallDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "synology_csi_call_duration_seconds",
		Help:    "Time until a CSI call returned a response.",
		Buckets: []float64{0.1, 0.5, 1, 5, 10, 30, 60, 120},
	}, []string{"operation"})
)

// Collectors returns the metrics the CSI servers record.
func Collectors() []prometheus.Collector {
	return []prometheus.Collector{csiCalls, csiCallDuration}
}

// metricsInterceptor times every CSI call and counts it under its code.
func metricsInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	started := time.Now()
	resp, err := handler(ctx, req)
	operation := operationName(info.FullMethod)
	csiCallDuration.WithLabelValues(operation).Observe(time.Since(started).Seconds())
	csiCalls.WithLabelValues(operation, status.Code(err).String()).Inc()
	return resp, err
}

// operationName is the CSI operation in a gRPC method name: CreateVolume
// out of /csi.v1.Controller/CreateVolume.
func operationName(fullMethod string) string {
	if i := strings.LastIndex(fullMethod, "/"); i != -1 {
		return fullMethod[i+1:]
	}
	return fullMethod
}
