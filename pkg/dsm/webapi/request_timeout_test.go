package webapi

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"
	"time"
)

// hangingDSM answers no request until the test ends.
func hangingDSM(t *testing.T) (*DSM, func()) {
	t.Helper()
	stop := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-stop:
		case <-r.Context().Done():
		}
	}))
	host, portText, _ := net.SplitHostPort(server.Listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	return &DSM{Ip: host, Port: port}, func() { close(stop); server.Close() }
}

// A DSM that stops answering fails the request instead of holding the CSI
// call that made it forever.
func TestARequestToAHungDSMTimesOut(t *testing.T) {
	dsm, stop := hangingDSM(t)
	defer stop()
	dsm.timeout = 50 * time.Millisecond

	started := time.Now()
	_, err := dsm.LunList()

	if err == nil {
		t.Fatal("LunList succeeded against a DSM that never answered")
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Errorf("LunList took %v to give up", elapsed)
	}
}

// A request stops when the CSI call that made it is canceled, before the
// client's own timeout.
func TestARequestStopsWithItsCallersContext(t *testing.T) {
	dsm, stop := hangingDSM(t)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := dsm.WithContext(ctx).LunList()

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("LunList = %v, want the context's deadline", err)
	}
}

// The timeout leaves room for the share busy retry's whole budget plus one
// last request, inside the provisioner's RPC timeout that
// TestRetryBudgetsFitTheirCallers records.
func TestTheRequestTimeoutFitsTheBusyRetry(t *testing.T) {
	const sidecarRPCTimeout = 120 * time.Second
	if total := shareBusyRetryTimeout + DefaultRequestTimeout; total >= sidecarRPCTimeout {
		t.Errorf("a busy retry that ends in a request that times out takes %v, past the sidecar's %v", total, sidecarRPCTimeout)
	}
}

// A context view shares the session and every setting of the DSM it came
// from, so a field added to DSM and left out of WithContext fails here.
func TestAContextViewKeepsTheDSMsSettings(t *testing.T) {
	dsm := &DSM{
		Ip: "nas", Port: 5001, Username: "k8s", Password: "secret", Https: true,
		Controller: "A", TLSCACert: "ca", TLSServerName: "nas.example", InsecureSkipVerify: true,
		SystemInfo: SystemInfo{Hostname: "nas", SupportNvmeof: true, FirmwareVer: "DSM 7"},
	}

	view := dsm.WithContext(context.Background())

	original, copied := reflect.ValueOf(dsm).Elem(), reflect.ValueOf(view).Elem()
	for i := 0; i < original.NumField(); i++ {
		field := original.Type().Field(i)
		if !field.IsExported() {
			continue
		}
		if !reflect.DeepEqual(original.Field(i).Interface(), copied.Field(i).Interface()) {
			t.Errorf("the view's %s is %v, want %v", field.Name, copied.Field(i).Interface(), original.Field(i).Interface())
		}
	}
	if view.shared() != dsm {
		t.Error("the view does not share the DSM's session")
	}
}
