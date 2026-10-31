package driver

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/SynologyOpenSource/synology-csi/pkg/dsm/common"
	"github.com/SynologyOpenSource/synology-csi/pkg/dsm/service"
)

// nodeServerOnFakeDSM returns a node server whose DSM maps the given LUN
// UUIDs, each to a target of its own, or fails every target listing when
// targetsFail is set.
func nodeServerOnFakeDSM(t *testing.T, luns []string, targetsFail bool) *nodeServer {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reply := func(data interface{}) {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "data": data})
		}
		query := r.URL.Query()
		switch query.Get("api") {
		case "SYNO.Core.Share":
			reply(map[string]interface{}{"shares": []interface{}{}})
		case "SYNO.Core.ISCSI.Target":
			if targetsFail {
				json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": map[string]int{"code": 100}})
				return
			}
			targets := []map[string]interface{}{}
			for _, lun := range luns {
				targets = append(targets, map[string]interface{}{
					"name":        "k8s-csi-pvc-" + lun,
					"iqn":         "iqn.2000-01.com.synology:nas.pvc-" + lun,
					"mapped_luns": []map[string]interface{}{{"lun_uuid": lun}},
				})
			}
			reply(map[string]interface{}{"targets": targets})
		case "SYNO.Core.ISCSI.LUN":
			uuid, _ := strconv.Unquote(query.Get("uuid"))
			reply(map[string]interface{}{"lun": map[string]interface{}{"name": "k8s-csi-pvc-" + uuid, "uuid": uuid}})
		default:
			reply(map[string]string{"sid": "a-session", "hostname": "nas", "support_nvmeof": "no"})
		}
	}))
	t.Cleanup(server.Close)

	host, portText, _ := net.SplitHostPort(server.Listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	dsms := service.NewDsmService()
	if err := dsms.AddDsm(common.ClientInfo{Host: host, Port: port, Username: "k8s", Password: "secret"}); err != nil {
		t.Fatal(err)
	}
	return &nodeServer{dsmService: dsms}
}

func TestAFailedFlushIsHarmlessOnlyWhenTheLunIsKnownToBeGone(t *testing.T) {
	cases := []struct {
		name        string
		luns        []string
		targetsFail bool
		harmless    bool
	}{
		{"the DSM deleted the LUN", []string{"another"}, false, true},
		{"the LUN still exists", []string{"this", "another"}, false, false},
		{"the DSM cannot say", nil, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ns := nodeServerOnFakeDSM(t, c.luns, c.targetsFail)

			if got := ns.lunWasDeleted("this"); got != c.harmless {
				t.Errorf("lunWasDeleted = %v, want %v", got, c.harmless)
			}
		})
	}
}
