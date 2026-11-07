package service

import (
	"context"
	"testing"
)

// serviceFor returns a service with the fake as its one DSM.
func serviceFor(t *testing.T, fake *fakeDSM) *DsmService {
	t.Helper()
	service := NewDsmService()
	if err := service.AddDsm(fake.start(t)); err != nil {
		t.Fatal(err)
	}
	return service
}

// A lookup reads every target and every LUN in one request each, however
// many LUNs the targets map.
func TestALookupListsTargetsAndLunsOnce(t *testing.T) {
	fake := newFakeDSM()
	var wanted string
	for _, name := range []string{"k8s-csi-pvc-a", "k8s-csi-pvc-b", "k8s-csi-pvc-c"} {
		wanted = fake.addMappedLun(name)
	}
	service := serviceFor(t, fake)

	volume, err := service.GetVolume(context.Background(), wanted)

	if err != nil || volume == nil || volume.VolumeId != wanted {
		t.Fatalf("GetVolume(%s) = (%v, %v), want the volume", wanted, volume, err)
	}
	for _, call := range []struct {
		api, method string
		want        int
	}{
		{"SYNO.Core.ISCSI.Target", "list", 1},
		{"SYNO.Core.ISCSI.LUN", "list", 1},
		{"SYNO.Core.ISCSI.LUN", "get", 0},
	} {
		if got := fake.count(call.api, call.method); got != call.want {
			t.Errorf("%s %s: %d requests, want %d", call.api, call.method, got, call.want)
		}
	}
}

// A mapped LUN the list leaves out is read on its own, so a LUN type the
// list does not ask for, or a LUN created between the two listings, is
// still found.
func TestAMappedLunMissingFromTheListIsReadOnItsOwn(t *testing.T) {
	fake := newFakeDSM()
	fake.addMappedLun("k8s-csi-pvc-listed")
	unlisted := fake.addMappedLun("k8s-csi-pvc-unlisted")
	fake.unlisted[unlisted] = true
	service := serviceFor(t, fake)

	volume, err := service.GetVolume(context.Background(), unlisted)

	if err != nil || volume == nil || volume.VolumeId != unlisted {
		t.Fatalf("GetVolume(%s) = (%v, %v), want the volume", unlisted, volume, err)
	}
	if got := fake.count("SYNO.Core.ISCSI.LUN", "get"); got != 1 {
		t.Errorf("%d LUN gets, want 1 for the unlisted LUN", got)
	}
}

// Only the DSM's own NoSuchLun proves a LUN is gone. A failed list proves
// nothing, so the lookup reports itself incomplete.
func TestALookupIsIncompleteWhenAListFails(t *testing.T) {
	for _, failing := range []string{
		"SYNO.Core.ISCSI.Target list",
		"SYNO.Core.ISCSI.LUN list",
	} {
		t.Run(failing, func(t *testing.T) {
			fake := newFakeDSM()
			fake.addMappedLun("k8s-csi-pvc-a")
			fake.failing[failing] = true
			service := serviceFor(t, fake)

			volume, err := service.GetVolume(context.Background(), "no-such-volume")

			if err == nil || volume != nil {
				t.Errorf("GetVolume = (%v, %v), want an error", volume, err)
			}
		})
	}
}

// A mapped LUN that is in neither the list nor a LunGet was deleted, and the
// lookup is complete without it. The LUNs mapped after it are still found.
func TestAMappedLunTheDSMNoLongerHasIsGone(t *testing.T) {
	fake := newFakeDSM()
	fake.mu.Lock()
	target := fake.createTarget("k8s-csi-pvc-a", "iqn.2000-01.com.synology:nas.pvc-a")
	fake.mapLun(target, "deleted")
	kept := fake.createLun("k8s-csi-pvc-a", "/volume1", 1<<30)
	fake.mapLun(target, kept)
	fake.mu.Unlock()
	service := serviceFor(t, fake)

	for _, lookup := range []struct {
		uuid  string
		found bool
	}{
		{"deleted", false},
		{kept, true},
	} {
		volume, err := service.GetVolume(context.Background(), lookup.uuid)

		if err != nil || (volume != nil) != lookup.found {
			t.Errorf("GetVolume(%s) = (%v, %v), want found=%v", lookup.uuid, volume, err, lookup.found)
		}
	}
}
