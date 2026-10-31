package service

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cenkalti/backoff/v4"

	"github.com/SynologyOpenSource/synology-csi/pkg/dsm/common"
)

// switchableDSM starts a fake DSM that answers 503 until up is set, and then
// answers a login and the system info that AddDsm reads.
func switchableDSM(t *testing.T) (common.ClientInfo, *atomic.Bool) {
	t.Helper()
	up := &atomic.Bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !up.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		data := map[string]string{"sid": "a-session", "hostname": "nas", "support_nvmeof": "no"}
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "data": data})
	}))
	t.Cleanup(server.Close)

	host, portText, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	return common.ClientInfo{Host: host, Port: port, Username: "k8s", Password: "secret"}, up
}

func TestAnUnreachableDSMIsNotAdded(t *testing.T) {
	client, _ := switchableDSM(t)
	service := NewDsmService()

	if err := service.AddDsm(client); err == nil {
		t.Fatal("AddDsm succeeded against a DSM that answers 503")
	}

	if got := service.GetDsmsCount(); got != 0 {
		t.Errorf("GetDsmsCount() = %d, want 0", got)
	}
}

func TestADSMIsAddedOnceItBecomesReachable(t *testing.T) {
	client, up := switchableDSM(t)
	service := NewDsmService()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	added := make(chan error, 1)
	go func() {
		added <- service.AddDsmWhenReachable(ctx, client, backoff.NewConstantBackOff(10*time.Millisecond))
	}()
	time.Sleep(50 * time.Millisecond)
	up.Store(true)

	if err := <-added; err != nil {
		t.Fatalf("AddDsmWhenReachable: %v", err)
	}
	dsm, err := service.GetDsm(client.Host)
	if err != nil {
		t.Fatalf("GetDsm: %v", err)
	}
	// The hostname names every iSCSI target, so a DSM must not be listed
	// before its system info is read.
	if dsm.Hostname != "nas" {
		t.Errorf("Hostname = %q, want %q", dsm.Hostname, "nas")
	}
}

func TestWaitingForADSMStopsWithItsContext(t *testing.T) {
	client, _ := switchableDSM(t)
	service := NewDsmService()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := service.AddDsmWhenReachable(ctx, client, backoff.NewConstantBackOff(10*time.Millisecond))

	if err == nil {
		t.Fatal("AddDsmWhenReachable returned nil for a DSM that never came up")
	}
	if got := service.GetDsmsCount(); got != 0 {
		t.Errorf("GetDsmsCount() = %d, want 0", got)
	}
}

func TestReadersCanListDSMsWhileOneIsAdded(t *testing.T) {
	client, up := switchableDSM(t)
	up.Store(true)
	service := NewDsmService()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for ctx.Err() == nil && service.GetDsmsCount() == 0 {
			service.ListDsmVolumes("")
		}
	}()

	if err := service.AddDsmWhenReachable(ctx, client, backoff.NewConstantBackOff(10*time.Millisecond)); err != nil {
		t.Fatalf("AddDsmWhenReachable: %v", err)
	}
	<-done
}
