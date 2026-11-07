package webapi

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
)

// expiringDSM rejects every session but the one its last login handed out,
// with the DSM's session-timeout error, and counts the logins.
func expiringDSM(t *testing.T) (*DSM, *atomic.Int32) {
	t.Helper()
	logins := &atomic.Int32{}
	var current atomic.Value
	current.Store("")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reply := func(data interface{}) {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "data": data})
		}
		if r.URL.Query().Get("api") == "SYNO.API.Auth" {
			sid := "session-" + strconv.Itoa(int(logins.Add(1)))
			current.Store(sid)
			reply(map[string]string{"sid": sid})
			return
		}
		cookie, err := r.Cookie("id")
		if err != nil || cookie.Value != current.Load().(string) {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": map[string]int{"code": 119}})
			return
		}
		reply(map[string]interface{}{"luns": []interface{}{}})
	}))
	t.Cleanup(server.Close)
	host, portText, _ := net.SplitHostPort(server.Listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	return &DSM{Ip: host, Port: port, sid: "expired"}, logins
}

// Requests that find the session expired at the same moment log in once
// between them, and each one then succeeds on the new session. A login per
// request would hand each one a session the next login replaces.
func TestConcurrentRequestsOnAnExpiredSessionLogInOnce(t *testing.T) {
	dsm, logins := expiringDSM(t)

	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := dsm.LunList()
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Errorf("LunList: %v", err)
		}
	}
	if got := logins.Load(); got != 1 {
		t.Errorf("%d logins, want 1", got)
	}
}
