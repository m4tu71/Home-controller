package services

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"backend/repositories"
)

func TestDataHealthScanFixture(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	put := func(rel, data string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	put("rooms/good/room.json", `{"id":"good"}`)
	put("rooms/good/devices/good/device.json", `{"id":"good","room_id":"good"}`)
	put("rooms/good/devices/good/adapters/ir/commands/bad.json", `{"format":"raw_median"}`)
	put("rooms/missing/devices/lost/device.json", `{"id":"lost"}`)
	put("rooms/good/devices/missing/adapters/ir/ir_config.json", `{}`)
	put("ir-signals/empty.json", "")
	put("ir-signals/broken.json", "{")
	put("ir-signals/old.json", `{"format":"raw_median"}`)
	if err := os.MkdirAll(filepath.Join(root, "rooms", "empty"), 0700); err != nil {
		t.Fatal(err)
	}
	before, err := repositories.NewDataHealthRepository(root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	issues, err := NewDataHealthService(repositories.NewDataHealthRepository(root)).Scan()
	if err != nil {
		t.Fatal(err)
	}
	after, err := repositories.NewDataHealthRepository(root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("scan modified fixture")
	}
	kinds := map[string]int{}
	for _, issue := range issues {
		kinds[strings.SplitN(issue.ID, ":", 2)[0]]++
		if !strings.HasPrefix(issue.CanonicalPath, root+string(filepath.Separator)) || issue.Hash == "" {
			t.Fatalf("invalid issue metadata: %+v", issue)
		}
	}
	for _, kind := range []string{"orphan_room", "orphan_device", "orphan_adapter", "missing_room_json", "missing_device_json", "empty_directory", "empty_json", "invalid_json", "unsupported_format"} {
		if kinds[kind] == 0 {
			t.Errorf("missing %s: %#v", kind, kinds)
		}
	}
	if len(issues) != 13 {
		t.Errorf("got %d issues: %#v", len(issues), kinds)
	}
}

func TestDataHealthRejectsTraversalAndDoesNotFollowSymlink(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "data")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(base, "outside.json")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked.json")); err != nil {
		t.Fatal(err)
	}
	issues, err := NewDataHealthService(repositories.NewDataHealthRepository(root)).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || !strings.HasPrefix(issues[0].ID, "unsafe_symlink:") || issues[0].HasDestructiveActions {
		t.Fatalf("unexpected issues: %+v", issues)
	}
	if _, err := repositories.NewDataHealthRepository(filepath.Join(root, "..", "outside.json")).Scan(); err == nil {
		t.Fatal("traversal accepted")
	}
	if _, err := repositories.NewDataHealthRepository(filepath.Join(root, "linked.json")).Scan(); err == nil {
		t.Fatal("non-directory root accepted")
	}
}
