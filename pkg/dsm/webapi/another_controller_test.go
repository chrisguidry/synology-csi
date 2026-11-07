package webapi

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

// A UC pair's DSM answers both controllers' interface lists, and both
// controllers carry this host's address.
func ucDSM(t *testing.T) *DSM {
	t.Helper()
	return answeringDSM(t, func(w http.ResponseWriter, r *http.Request) {
		ifaces := []NetworkInterface{{Ifname: "eth0", Ip: "127.0.0.1", Status: "connected"}}
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "data": ifaces})
	})
}

// Every node stage of a UC volume asks for the other controller, from
// concurrent CSI calls on one DSM. Asking must not write to that DSM.
func TestConcurrentCallsCanAskForTheOtherController(t *testing.T) {
	dsm := ucDSM(t)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			other, err := dsm.GetAnotherController()
			if err != nil || other.Ip != "127.0.0.1" || other.Controller != "A" {
				t.Errorf("GetAnotherController = (%+v, %v), want controller A at 127.0.0.1", other, err)
			}
		}()
	}
	wg.Wait()
}

// A reachability check closes the connection it opened, so checking does
// not leave a socket open on the node and the DSM for each stage.
func TestAReachabilityCheckClosesItsConnection(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	if !CheckIpReachable("127.0.0.1", listener.Addr().(*net.TCPAddr).Port) {
		t.Fatal("CheckIpReachable reported a listening port unreachable")
	}

	conn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var netErr net.Error
	if _, err := conn.Read(make([]byte, 1)); err == nil || errors.As(err, &netErr) {
		t.Errorf("the check's connection is still open: read returned %v, want EOF", err)
	}
}
