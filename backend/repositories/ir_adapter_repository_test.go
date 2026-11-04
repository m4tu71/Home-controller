package repositories

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"backend/models"
)

func TestIRAdapterRepositoryAtomicFamilyFailureAndReadback(t *testing.T) {
	const roomID, deviceID = "bedroom", "remote"
	oldConfig := models.IRConfig{IRRemoteType: models.IRRemoteTypeCommand, CarrierKHz: 38}
	nextConfig := models.IRConfig{IRRemoteType: models.IRRemoteTypeCommand, CarrierKHz: 40}
	oldCommand := models.IRCommand{ID: "power", Name: "Old", Format: models.OperationFormatRawSamples, Captures: []models.IRCapture{{Attempt: 1, Valid: true, TimingsUS: []uint{100, 200}}}}
	nextCommand := oldCommand
	nextCommand.Name = "New"
	oldSchema := models.IRStateSchema{"power": {Type: models.IRStateFieldTypeBoolean}}
	nextSchema := models.IRStateSchema{"mode": {Type: models.IRStateFieldTypeString}}
	oldState := models.IRState{ID: "cool", Name: "Old", State: map[string]interface{}{"power": true}, Format: models.OperationFormatRawSamples, Captures: []models.IRCapture{{Attempt: 1, Valid: true, TimingsUS: []uint{300, 400}}}}
	nextState := oldState
	nextState.Name = "New"
	type family struct {
		name     string
		path     func(IRAdapterRepository) string
		saveOld  func(IRAdapterRepository) error
		saveNext func(IRAdapterRepository) error
		load     func(IRAdapterRepository) (any, error)
	}
	families := []family{
		{"config", func(r IRAdapterRepository) string { return r.configPath(roomID, deviceID) }, func(r IRAdapterRepository) error { return r.SaveConfig(roomID, deviceID, oldConfig) }, func(r IRAdapterRepository) error { return r.SaveConfig(roomID, deviceID, nextConfig) }, func(r IRAdapterRepository) (any, error) { return r.LoadConfig(roomID, deviceID) }},
		{"command", func(r IRAdapterRepository) string { return r.commandPath(roomID, deviceID, "power") }, func(r IRAdapterRepository) error { return r.SaveCommand(roomID, deviceID, oldCommand) }, func(r IRAdapterRepository) error { return r.SaveCommand(roomID, deviceID, nextCommand) }, func(r IRAdapterRepository) (any, error) { return r.LoadCommand(roomID, deviceID, "power") }},
		{"schema", func(r IRAdapterRepository) string { return r.stateSchemaPath(roomID, deviceID) }, func(r IRAdapterRepository) error { return r.SaveStateSchema(roomID, deviceID, oldSchema) }, func(r IRAdapterRepository) error { return r.SaveStateSchema(roomID, deviceID, nextSchema) }, func(r IRAdapterRepository) (any, error) { return r.LoadStateSchema(roomID, deviceID) }},
		{"state", func(r IRAdapterRepository) string { return r.statePath(roomID, deviceID, "cool") }, func(r IRAdapterRepository) error { return r.SaveState(roomID, deviceID, oldState) }, func(r IRAdapterRepository) error { return r.SaveState(roomID, deviceID, nextState) }, func(r IRAdapterRepository) (any, error) { return r.LoadState(roomID, deviceID, "cool") }},
	}
	stages := []AtomicWriteStage{AtomicWriteCreateTemp, AtomicWriteWrite, AtomicWriteChmod, AtomicWriteFileSync, AtomicWriteFileClose, AtomicWriteRename, AtomicWriteParentSync}
	for _, target := range families {
		for _, stage := range stages {
			t.Run(target.name+"/"+string(stage), func(t *testing.T) {
				rooms := t.TempDir()
				repo := NewIRAdapterRepositoryWithRoomsDir(rooms)
				original := make(map[string][]byte)
				for _, f := range families {
					if err := f.saveOld(repo); err != nil {
						t.Fatal(err)
					}
					data, err := os.ReadFile(f.path(repo))
					if err != nil {
						t.Fatal(err)
					}
					original[f.name] = data
				}
				injected := errors.New("injected IR write failure")
				repo.writer = &AtomicJSONWriter{beforeStage: func(current AtomicWriteStage) error {
					if current == stage {
						return injected
					}
					return nil
				}}
				err := target.saveNext(repo)
				var writeErr *AtomicWriteError
				if !errors.As(err, &writeErr) || !errors.Is(err, injected) || writeErr.Stage != stage || writeErr.TargetReplaced != (stage == AtomicWriteParentSync) {
					t.Fatalf("unexpected write boundary: %v", err)
				}
				fresh := NewIRAdapterRepositoryWithRoomsDir(rooms)
				for _, f := range families {
					data, err := os.ReadFile(f.path(fresh))
					if err != nil {
						t.Fatal(err)
					}
					if f.name != target.name || stage != AtomicWriteParentSync {
						if !reflect.DeepEqual(data, original[f.name]) {
							t.Fatalf("%s changed unexpectedly during %s failure", f.name, target.name)
						}
					}
					if _, err := f.load(fresh); err != nil {
						t.Fatalf("fresh repository cannot load %s: %v", f.name, err)
					}
				}
				if stage == AtomicWriteParentSync {
					data, _ := os.ReadFile(target.path(fresh))
					if reflect.DeepEqual(data, original[target.name]) {
						t.Fatal("renamed target did not contain new JSON")
					}
				}
				entries, err := os.ReadDir(filepath.Dir(target.path(fresh)))
				if err != nil {
					t.Fatal(err)
				}
				for _, entry := range entries {
					if filepath.Ext(entry.Name()) == ".tmp" || entry.Name()[0] == '.' {
						t.Fatalf("temporary file remains: %s", entry.Name())
					}
				}
			})
		}
	}
}

func TestIRAdapterRepositoryFourFamilyCreateUpdateReadback(t *testing.T) {
	rooms := t.TempDir()
	repo := NewIRAdapterRepositoryWithRoomsDir(rooms)
	config := models.IRConfig{IRRemoteType: models.IRRemoteTypeStateful, CarrierKHz: 38}
	command := models.IRCommand{ID: "power", Name: "Power", Format: models.OperationFormatRawSamples, Captures: []models.IRCapture{{Attempt: 1, Valid: true, TimingsUS: []uint{100, 200}}}}
	schema := models.IRStateSchema{"power": {Type: models.IRStateFieldTypeBoolean}}
	state := models.IRState{ID: "cool", Name: "Cool", State: map[string]interface{}{"power": true}, Format: models.OperationFormatRawSamples, Captures: []models.IRCapture{{Attempt: 1, Valid: true, TimingsUS: []uint{300, 400}}}}
	for _, name := range []string{"initial", "updated"} {
		command.Name = name
		state.Name = name
		if err := repo.SaveConfig("bedroom", "remote", config); err != nil {
			t.Fatal(err)
		}
		if err := repo.SaveCommand("bedroom", "remote", command); err != nil {
			t.Fatal(err)
		}
		if err := repo.SaveStateSchema("bedroom", "remote", schema); err != nil {
			t.Fatal(err)
		}
		if err := repo.SaveState("bedroom", "remote", state); err != nil {
			t.Fatal(err)
		}
		fresh := NewIRAdapterRepositoryWithRoomsDir(rooms)
		gotConfig, err := fresh.LoadConfig("bedroom", "remote")
		if err != nil || !reflect.DeepEqual(gotConfig, config) {
			t.Fatalf("config readback: %+v, %v", gotConfig, err)
		}
		gotCommand, err := fresh.LoadCommand("bedroom", "remote", "power")
		if err != nil || !reflect.DeepEqual(gotCommand, command) {
			t.Fatalf("command readback: %+v, %v", gotCommand, err)
		}
		gotSchema, err := fresh.LoadStateSchema("bedroom", "remote")
		if err != nil || !reflect.DeepEqual(gotSchema, schema) {
			t.Fatalf("schema readback: %+v, %v", gotSchema, err)
		}
		gotState, err := fresh.LoadState("bedroom", "remote", "cool")
		if err != nil || !reflect.DeepEqual(gotState, state) {
			t.Fatalf("state readback: %+v, %v", gotState, err)
		}
	}
}
