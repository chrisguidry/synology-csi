package webapi

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// answeringDSM serves every request with the handler and returns a DSM that
// reaches it with a short request timeout.
func answeringDSM(t *testing.T, handler http.HandlerFunc) *DSM {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	host, portText, _ := net.SplitHostPort(server.Listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	return &DSM{Ip: host, Port: port, timeout: 100 * time.Millisecond}
}

func TestEachDSMRequestIsCountedByItsOutcome(t *testing.T) {
	for _, c := range []struct {
		outcome   string
		reachable float64
		handler   http.HandlerFunc
	}{
		{"success", 1, func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "data": map[string]interface{}{"luns": []interface{}{}}})
		}},
		{"dsm_error", 1, func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": map[string]int{"code": 18990001}})
		}},
		{"http_error", 1, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}},
		{"timeout", 0, func(w http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		}},
	} {
		t.Run(c.outcome, func(t *testing.T) {
			dsm := answeringDSM(t, c.handler)
			counted := dsmRequests.WithLabelValues(dsm.Ip, "SYNO.Core.ISCSI.LUN", "list", c.outcome)
			before := testutil.ToFloat64(counted)

			dsm.LunList()

			if got := testutil.ToFloat64(counted) - before; got != 1 {
				t.Errorf("counted %v LUN lists with outcome %s, want 1", got, c.outcome)
			}
			if got := testutil.ToFloat64(dsmReachable.WithLabelValues(dsm.Ip)); got != c.reachable {
				t.Errorf("reachable = %v, want %v", got, c.reachable)
			}
		})
	}
}

// A DSM that nothing listens on counts as unreachable.
func TestARequestNothingAnswersCountsAsUnreachable(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	dsm := &DSM{Ip: "127.0.0.1", Port: port}
	counted := dsmRequests.WithLabelValues(dsm.Ip, "SYNO.Core.ISCSI.LUN", "list", "unreachable")
	before := testutil.ToFloat64(counted)

	dsm.LunList()

	if got := testutil.ToFloat64(counted) - before; got != 1 {
		t.Errorf("counted %v unreachable LUN lists, want 1", got)
	}
	if got := testutil.ToFloat64(dsmReachable.WithLabelValues(dsm.Ip)); got != 0 {
		t.Errorf("reachable = %v, want 0", got)
	}
}
