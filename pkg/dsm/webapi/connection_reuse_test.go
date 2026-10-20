package webapi

import (
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// countingDSM starts a fake DSM that counts the TCP connections clients open
// to it, and returns a DSM client pointed at it.
func countingDSM(t *testing.T, https bool) (*DSM, *int64) {
	t.Helper()
	var opened int64
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// A real DSM ends its JSON with a newline, which the JSON decoder
		// leaves unread.
		w.Write([]byte(fakeDSMResponse + "\n"))
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			atomic.AddInt64(&opened, 1)
		}
	}
	t.Cleanup(server.Close)

	certPEM := ""
	if https {
		var tlsCert tls.Certificate
		certPEM, tlsCert = generateSelfSignedCert(t)
		server.TLS = &tls.Config{Certificates: []tls.Certificate{tlsCert}}
		server.StartTLS()
	} else {
		server.Start()
	}

	host, port := serverHostPort(t, server)
	return &DSM{
		Ip:        host,
		Port:      port,
		Username:  "admin",
		Password:  "secret",
		Https:     https,
		TLSCACert: certPEM,
	}, &opened
}

func TestRequestsReuseOneConnection(t *testing.T) {
	cases := []struct {
		name  string
		https bool
	}{
		{"https", true},
		{"http", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dsm, opened := countingDSM(t, c.https)

			for i := 0; i < 5; i++ {
				if err := dsm.Login(); err != nil {
					t.Fatalf("login %d failed: %v", i, err)
				}
			}

			if got := atomic.LoadInt64(opened); got != 1 {
				t.Errorf("five requests opened %d connections, want 1", got)
			}
		})
	}
}
