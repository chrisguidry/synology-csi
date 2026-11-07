package driver

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Every CSI call is counted under its operation and the gRPC code it
// returned, and timed.
func TestEveryCSICallIsCountedByItsCode(t *testing.T) {
	for _, c := range []struct {
		code codes.Code
		err  error
	}{
		{codes.OK, nil},
		{codes.Unavailable, status.Error(codes.Unavailable, "the DSM could not be listed")},
		{codes.Unknown, context.Canceled},
	} {
		t.Run(c.code.String(), func(t *testing.T) {
			counted := csiCalls.WithLabelValues("CreateVolume", c.code.String())
			before := testutil.ToFloat64(counted)
			call := &grpc.UnaryServerInfo{FullMethod: "/csi.v1.Controller/CreateVolume"}
			handle := func(ctx context.Context, request interface{}) (interface{}, error) { return nil, c.err }

			_, err := metricsInterceptor(context.Background(), nil, call, handle)

			if err != c.err {
				t.Errorf("the interceptor returned %v, want the handler's %v", err, c.err)
			}
			if got := testutil.ToFloat64(counted) - before; got != 1 {
				t.Errorf("counted %v CreateVolume calls with code %s, want 1", got, c.code)
			}
		})
	}
	if got := testutil.CollectAndCount(csiCallDuration, "synology_csi_call_duration_seconds"); got == 0 {
		t.Error("no CSI call duration was recorded")
	}
}
