package service

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"github.com/SynologyOpenSource/synology-csi/pkg/dsm/common"
)

// fakeDSM answers the WebAPI requests an iSCSI volume's create, lookup, and
// delete make, from LUNs and targets it keeps in memory, and counts each
// request by API and method.
type fakeDSM struct {
	mu       sync.Mutex
	luns     []fakeLun
	targets  []fakeTarget
	requests map[string]int
	nextId   int

	// unlisted names LUNs, by UUID, that the LUN list leaves out, as a DSM
	// does for a LUN type the list does not ask for.
	unlisted map[string]bool
	// failing names requests, as "api method", that answer with a DSM error.
	failing map[string]bool
	// stall is called before a request is answered, so a test can hold a
	// request past its caller's deadline. The DSM still carries it out.
	stall func(api, method string)
}

type fakeLun struct {
	Name     string `json:"name"`
	Uuid     string `json:"uuid"`
	Location string `json:"location"`
	Size     int64  `json:"size"`
}

type fakeMapping struct {
	LunUuid      string `json:"lun_uuid"`
	MappingIndex int    `json:"mapping_index"`
}

type fakeTarget struct {
	Name       string        `json:"name"`
	Iqn        string        `json:"iqn"`
	TargetId   int           `json:"target_id"`
	MappedLuns []fakeMapping `json:"mapped_luns"`
}

func newFakeDSM() *fakeDSM {
	return &fakeDSM{
		requests: map[string]int{},
		unlisted: map[string]bool{},
		failing:  map[string]bool{},
	}
}

// start serves the fake and returns the client info that reaches it.
func (f *fakeDSM) start(t *testing.T) common.ClientInfo {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(server.Close)
	host, portText, _ := net.SplitHostPort(server.Listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	return common.ClientInfo{Host: host, Port: port, Username: "k8s", Password: "secret"}
}

// addMappedLun adds a LUN and a target that maps it, as a finished
// CreateVolume leaves them.
func (f *fakeDSM) addMappedLun(name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	uuid := f.createLun(name, "/volume1", 1<<30)
	id := f.createTarget(name, "iqn.2000-01.com.synology:nas."+name)
	f.mapLun(id, uuid)
	return uuid
}

// count reports how many requests the fake answered for an API and method.
func (f *fakeDSM) count(api, method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[api+" "+method]
}

func (f *fakeDSM) lunCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.luns)
}

func (f *fakeDSM) createLun(name, location string, size int64) string {
	f.nextId++
	uuid := fmt.Sprintf("lun-%d", f.nextId)
	f.luns = append(f.luns, fakeLun{Name: name, Uuid: uuid, Location: location, Size: size})
	return uuid
}

func (f *fakeDSM) createTarget(name, iqn string) int {
	f.nextId++
	f.targets = append(f.targets, fakeTarget{Name: name, Iqn: iqn, TargetId: f.nextId})
	return f.nextId
}

func (f *fakeDSM) mapLun(targetId int, uuid string) {
	for i := range f.targets {
		if f.targets[i].TargetId == targetId {
			f.targets[i].MappedLuns = append(f.targets[i].MappedLuns, fakeMapping{LunUuid: uuid, MappingIndex: 1})
		}
	}
}

// findLun matches a UUID or a name, because the driver's LunGet passes
// either one in the uuid parameter.
func (f *fakeDSM) findLun(key string) (fakeLun, bool) {
	for _, lun := range f.luns {
		if lun.Uuid == key || lun.Name == key {
			return lun, true
		}
	}
	return fakeLun{}, false
}

func (f *fakeDSM) findTarget(key string) (fakeTarget, bool) {
	for _, target := range f.targets {
		if strconv.Itoa(target.TargetId) == key || target.Name == key {
			return target, true
		}
	}
	return fakeTarget{}, false
}

func unquote(value string) string {
	if unquoted, err := strconv.Unquote(value); err == nil {
		return unquoted
	}
	return value
}

func (f *fakeDSM) serve(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	api, method := query.Get("api"), query.Get("method")
	if f.stall != nil {
		f.stall(api, method)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests[api+" "+method]++

	reply := func(data interface{}) {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "data": data})
	}
	fail := func(code int) {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": map[string]int{"code": code}})
	}
	if f.failing[api+" "+method] {
		fail(18990000 + 1)
		return
	}

	switch api + " " + method {
	case "SYNO.API.Auth login":
		reply(map[string]string{"sid": "a-session"})
	case "SYNO.Core.System info":
		reply(map[string]string{"hostname": "nas", "support_nvmeof": "no"})
	case "SYNO.Core.Share list":
		reply(map[string]interface{}{"shares": []interface{}{}})
	case "SYNO.Core.Storage.Volume get":
		reply(map[string]interface{}{"volume": map[string]string{
			"volume_path": unquote(query.Get("volume_path")), "fs_type": "btrfs",
		}})
	case "SYNO.Core.ISCSI.LUN list":
		listed := []fakeLun{}
		for _, lun := range f.luns {
			if !f.unlisted[lun.Uuid] {
				listed = append(listed, lun)
			}
		}
		reply(map[string]interface{}{"luns": listed})
	case "SYNO.Core.ISCSI.LUN get":
		lun, ok := f.findLun(unquote(query.Get("uuid")))
		if !ok {
			fail(18990531)
			return
		}
		reply(map[string]interface{}{"lun": lun})
	case "SYNO.Core.ISCSI.LUN create":
		name := unquote(query.Get("name"))
		if _, ok := f.findLun(name); ok {
			fail(18990538)
			return
		}
		size, _ := strconv.ParseInt(query.Get("size"), 10, 64)
		reply(map[string]string{"uuid": f.createLun(name, query.Get("location"), size)})
	case "SYNO.Core.ISCSI.LUN delete":
		uuid := unquote(query.Get("uuid"))
		for i, lun := range f.luns {
			if lun.Uuid == uuid {
				f.luns = append(f.luns[:i], f.luns[i+1:]...)
				reply(struct{}{})
				return
			}
		}
		fail(18990531)
	case "SYNO.Core.ISCSI.LUN map_target":
		var ids []int
		json.Unmarshal([]byte(query.Get("target_ids")), &ids)
		for _, id := range ids {
			f.mapLun(id, unquote(query.Get("uuid")))
		}
		reply(struct{}{})
	case "SYNO.Core.ISCSI.Target list":
		reply(map[string]interface{}{"targets": f.targets})
	case "SYNO.Core.ISCSI.Target get":
		target, ok := f.findTarget(unquote(query.Get("target_id")))
		if !ok {
			fail(18990000 + 710)
			return
		}
		reply(map[string]interface{}{"target": target})
	case "SYNO.Core.ISCSI.Target create":
		name := query.Get("name")
		if _, ok := f.findTarget(name); ok {
			fail(18990744)
			return
		}
		reply(map[string]int{"target_id": f.createTarget(name, query.Get("iqn"))})
	default:
		reply(struct{}{})
	}
}
