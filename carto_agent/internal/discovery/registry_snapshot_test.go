package discovery

import (
	"fmt"
	"sync"
	"testing"
)

func snapshotRegistry() *InstanceRegistry {
	return &InstanceRegistry{instances: map[string]*QGISInstance{}, eventCh: make(chan InstanceEvent, 4096), stopCh: make(chan struct{})}
}

func TestInstanceRegistrySnapshotsDoNotAliasMutableState(t *testing.T) {
	r := snapshotRegistry()
	if err := r.Register(RegisterRequest{InstanceID: "snapshot", ProjectPath: "before.qgz"}); err != nil {
		t.Fatal(err)
	}
	added := <-r.Events()
	snapshot := r.Get("snapshot")
	snapshot.ProjectPath = "caller.qgz"
	delete(snapshot.Services, ServiceNameQGISBridge)
	all := r.GetAll()
	all[0].ProjectPath = "all-caller.qgz"
	if got := r.Get("snapshot"); got.ProjectPath != "before.qgz" || len(got.Services) == 0 {
		t.Fatalf("caller mutated registry: %+v", got)
	}
	if err := r.HandleEvent("snapshot", EventRequest{EventType: "project_saved", Data: map[string]interface{}{"project_path": "after.qgz"}}); err != nil {
		t.Fatal(err)
	}
	changed := <-r.Events()
	if err := r.Heartbeat("snapshot"); err != nil {
		t.Fatal(err)
	}
	<-r.Events()
	if err := r.Unregister("snapshot"); err != nil {
		t.Fatal(err)
	}
	removed := <-r.Events()
	if added.Instance.ProjectPath != "before.qgz" || added.Instance.Status != "running" || !added.Instance.UpdatedAt.Equal(added.Instance.CreatedAt) {
		t.Fatalf("added event was changed by subsequent writes: %+v", added.Instance)
	}
	if changed.Instance.ProjectPath != "after.qgz" || changed.Instance.Status != "running" {
		t.Fatalf("changed event was changed by unregister: %+v", changed.Instance)
	}
	if removed.Instance.Status != "stopped" {
		t.Fatal(removed.Instance)
	}
}

func TestInstanceRegistryConcurrentReadersAndEventConsumers(t *testing.T) {
	r := snapshotRegistry()
	var work, events sync.WaitGroup
	events.Add(1)
	go func() {
		defer events.Done()
		for event := range r.eventCh {
			event.Instance.ProjectPath = "consumer.qgz"
			event.Instance.Services["consumer"] = ServiceInfo{Status: "local"}
		}
	}()
	work.Add(1)
	go func() {
		defer work.Done()
		for i := 0; i < 100; i++ {
			if err := r.Register(RegisterRequest{InstanceID: "concurrent", ProjectPath: "registered.qgz"}); err != nil {
				t.Error(err)
			}
			if err := r.HandleEvent("concurrent", EventRequest{EventType: "project_saved", Data: map[string]interface{}{"project_path": fmt.Sprintf("%d.qgz", i)}}); err != nil {
				t.Error(err)
			}
			if err := r.Heartbeat("concurrent"); err != nil {
				t.Error(err)
			}
			if err := r.Unregister("concurrent"); err != nil {
				t.Error(err)
			}
		}
	}()
	for reader := 0; reader < 3; reader++ {
		work.Add(1)
		go func() {
			defer work.Done()
			for i := 0; i < 400; i++ {
				if inst := r.Get("concurrent"); inst != nil {
					inst.ProjectPath = "reader.qgz"
					inst.Services["reader"] = ServiceInfo{Status: "local"}
				}
				for _, inst := range r.GetAll() {
					inst.ProjectPath = "all-reader.qgz"
					delete(inst.Services, ServiceNameQGISBridge)
				}
			}
		}()
	}
	work.Wait()
	close(r.eventCh)
	events.Wait()
}
