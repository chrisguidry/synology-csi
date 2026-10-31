package service

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/SynologyOpenSource/synology-csi/pkg/dsm/common"
)

// dsmWithADeletedLun starts a fake DSM whose one target still maps two LUNs,
// one of which the DSM has already deleted, as happens when a LUN is deleted
// between the target listing and the LUN lookup.
func dsmWithADeletedLun(t *testing.T) common.ClientInfo {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reply := func(data interface{}) {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "data": data})
		}
		query := r.URL.Query()
		switch query.Get("api") {
		case "SYNO.Core.ISCSI.Target":
			reply(map[string]interface{}{"targets": []map[string]interface{}{{
				"name": "k8s-csi-pvc-kept",
				"iqn":  "iqn.2000-01.com.synology:nas.pvc-kept",
				"mapped_luns": []map[string]interface{}{
					{"lun_uuid": "kept"},
					{"lun_uuid": "gone"},
				},
			}}})
		case "SYNO.Core.ISCSI.LUN":
			if query.Get("uuid") == strconv.Quote("gone") {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false, "error": map[string]int{"code": 18990531},
				})
				return
			}
			reply(map[string]interface{}{"lun": map[string]interface{}{
				"name": "k8s-csi-pvc-kept", "uuid": "kept", "location": "/volume1",
			}})
		default:
			reply(map[string]string{"sid": "a-session", "hostname": "nas"})
		}
	}))
	t.Cleanup(server.Close)

	host, portText, _ := net.SplitHostPort(server.Listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	return common.ClientInfo{Host: host, Port: port, Username: "k8s", Password: "secret"}
}

func TestADeletedLunIsLeftOutOfTheListing(t *testing.T) {
	service := NewDsmService()
	if err := service.AddDsm(dsmWithADeletedLun(t)); err != nil {
		t.Fatal(err)
	}

	volumes, err := service.listISCSIVolumes("")

	if err != nil {
		t.Fatalf("listing failed because of a LUN the DSM already deleted: %v", err)
	}
	if len(volumes) != 1 || volumes[0].VolumeId != "kept" {
		t.Errorf("listed %d volumes, want only the one that still exists", len(volumes))
	}
}

func TestADeletedLunIsKnownNotToExist(t *testing.T) {
	service := NewDsmService()
	if err := service.AddDsm(dsmWithADeletedLun(t)); err != nil {
		t.Fatal(err)
	}

	volume, err := service.GetVolume("gone")

	// (nil, nil) tells NodeUnstageVolume and DeleteVolume the volume is gone,
	// where an error makes them fail and be called again.
	if err != nil || volume != nil {
		t.Errorf("GetVolume(gone) = (%v, %v), want (nil, nil)", volume, err)
	}
}
