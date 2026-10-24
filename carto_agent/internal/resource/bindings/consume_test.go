package bindings

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestConsumeBacksUpBeforeDestinationAndRetriesAfterInterruptedWrite(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "bindings.yaml")
	key := "flow:sample-flow/path:data/list.csv"
	value := filepath.Join(root, "data")
	original := "apiVersion: resource-bindings/v2\nbindings:\n  " + key + ": " + value + "\norphaned: {}\n"
	if err := os.WriteFile(file, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	store := New(file)
	expected := map[string]string{key: value}
	destination := filepath.Join(root, "destination")
	err := store.Consume(expected, func() error {
		backups, _ := filepath.Glob(filepath.Join(root, "backups/resource_bindings-*.yaml"))
		if len(backups) != 1 {
			t.Fatal("destination write preceded backup")
		}
		if err := os.WriteFile(destination, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		return errors.New("simulated interruption after destination write")
	})
	if err == nil {
		t.Fatal("interruption ignored")
	}
	after, _ := os.ReadFile(file)
	if string(after) != original {
		t.Fatal("source removed before destination confirmed")
	}
	if err := store.Consume(expected, func() error {
		data, err := os.ReadFile(destination)
		if err != nil || string(data) != value {
			t.Fatal("lost saved destination", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := store.Snapshot()
	if len(snapshot.Bindings) != 0 {
		t.Fatal(snapshot)
	}
}

func TestConsumeRejectsChangedSourceWithoutCallingDestination(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "bindings.yaml")
	key := "flow:sample-flow/path:data/list.csv"
	original := "apiVersion: resource-bindings/v2\nbindings:\n  " + key + ": /newer\norphaned: {}\n"
	if err := os.WriteFile(file, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	called := false
	err := New(file).Consume(map[string]string{key: "/older"}, func() error { called = true; return nil })
	if err == nil || called {
		t.Fatal("consumed a concurrent change", err, called)
	}
	after, _ := os.ReadFile(file)
	if string(after) != original {
		t.Fatal("source changed")
	}
}
