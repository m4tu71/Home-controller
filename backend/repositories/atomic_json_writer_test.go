package repositories

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type atomicWriterFixture struct {
	Version int    `json:"version"`
	Payload string `json:"payload"`
}

func TestAtomicJSONWriterCreatesAndUpdatesWithRequestedMode(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "record.json")
	writer := NewAtomicJSONWriter()

	if err := writer.WriteJSON(target, atomicWriterFixture{Version: 1, Payload: "initial"}, 0600); err != nil {
		t.Fatalf("create JSON: %v", err)
	}
	assertAtomicJSON(t, target, atomicWriterFixture{Version: 1, Payload: "initial"})
	assertFileMode(t, target, 0600)
	assertOnlyTargetFile(t, dir)

	if err := writer.WriteJSON(target, atomicWriterFixture{Version: 2, Payload: "updated"}, 0644); err != nil {
		t.Fatalf("update JSON: %v", err)
	}
	assertAtomicJSON(t, target, atomicWriterFixture{Version: 2, Payload: "updated"})
	assertFileMode(t, target, 0644)
	assertOnlyTargetFile(t, dir)
}

func TestAtomicJSONWriterCreatesTempInTargetDirectory(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "record.json")
	sawTemp := false
	writer := &AtomicJSONWriter{beforeStage: func(stage AtomicWriteStage) error {
		if stage != AtomicWriteWrite {
			return nil
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".record.json.tmp-") {
				sawTemp = true
			}
		}
		return nil
	}}
	if err := writer.WriteJSON(target, atomicWriterFixture{Version: 1}, 0600); err != nil {
		t.Fatalf("write JSON: %v", err)
	}
	if !sawTemp {
		t.Fatal("temporary file was not created in the target directory")
	}
	assertOnlyTargetFile(t, dir)
}

func TestAtomicJSONWriterPreRenameFailuresPreserveTargetAndCleanTemp(t *testing.T) {
	stages := []AtomicWriteStage{
		AtomicWriteCreateTemp,
		AtomicWriteWrite,
		AtomicWriteChmod,
		AtomicWriteFileSync,
		AtomicWriteFileClose,
		AtomicWriteRename,
	}

	for _, stage := range stages {
		t.Run(string(stage), func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "record.json")
			old := atomicWriterFixture{Version: 1, Payload: "existing"}
			if err := NewAtomicJSONWriter().WriteJSON(target, old, 0600); err != nil {
				t.Fatalf("seed target: %v", err)
			}

			injected := errors.New("injected stage failure")
			writer := &AtomicJSONWriter{beforeStage: func(current AtomicWriteStage) error {
				if current == stage {
					return injected
				}
				return nil
			}}
			err := writer.WriteJSON(target, atomicWriterFixture{Version: 2, Payload: "replacement"}, 0600)
			assertAtomicWriteError(t, err, injected, stage, false)
			assertAtomicJSON(t, target, old)
			assertOnlyTargetFile(t, dir)
		})
	}
}

func TestAtomicJSONWriterPartialWriteFailurePreservesTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "record.json")
	old := atomicWriterFixture{Version: 1, Payload: "existing"}
	if err := NewAtomicJSONWriter().WriteJSON(target, old, 0600); err != nil {
		t.Fatalf("seed target: %v", err)
	}

	injected := errors.New("partial write failed")
	writer := &AtomicJSONWriter{writeTemp: func(temp *os.File, data []byte) (int, error) {
		n, err := temp.Write(data[:len(data)/2])
		if err != nil {
			return n, err
		}
		return n, injected
	}}
	err := writer.WriteJSON(target, atomicWriterFixture{Version: 2, Payload: strings.Repeat("x", 512)}, 0600)
	assertAtomicWriteError(t, err, injected, AtomicWriteWrite, false)
	assertAtomicJSON(t, target, old)
	assertOnlyTargetFile(t, dir)
}

func TestAtomicJSONWriterParentSyncFailureReportsReplacedTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "record.json")
	old := atomicWriterFixture{Version: 1, Payload: "existing"}
	next := atomicWriterFixture{Version: 2, Payload: "replacement"}
	if err := NewAtomicJSONWriter().WriteJSON(target, old, 0600); err != nil {
		t.Fatalf("seed target: %v", err)
	}

	injected := errors.New("parent sync failed")
	writer := &AtomicJSONWriter{beforeStage: func(stage AtomicWriteStage) error {
		if stage == AtomicWriteParentSync {
			return injected
		}
		return nil
	}}
	err := writer.WriteJSON(target, next, 0600)
	assertAtomicWriteError(t, err, injected, AtomicWriteParentSync, true)
	assertAtomicJSON(t, target, next)
	assertOnlyTargetFile(t, dir)
}

func TestAtomicJSONWriterCleanupFailureIsReported(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "record.json")
	old := atomicWriterFixture{Version: 1, Payload: "existing"}
	if err := NewAtomicJSONWriter().WriteJSON(target, old, 0600); err != nil {
		t.Fatalf("seed target: %v", err)
	}

	writeFailure := errors.New("write failed")
	cleanupFailure := errors.New("cleanup failed")
	writer := &AtomicJSONWriter{beforeStage: func(stage AtomicWriteStage) error {
		switch stage {
		case AtomicWriteWrite:
			return writeFailure
		case AtomicWriteCleanup:
			return cleanupFailure
		default:
			return nil
		}
	}}
	err := writer.WriteJSON(target, atomicWriterFixture{Version: 2}, 0600)
	assertAtomicWriteError(t, err, writeFailure, AtomicWriteWrite, false)
	if !errors.Is(err, cleanupFailure) {
		t.Fatalf("error %v does not include cleanup failure", err)
	}
	assertAtomicJSON(t, target, old)
}

func TestAtomicJSONWriterRejectsInvalidInputWithoutCreatingTemp(t *testing.T) {
	tests := []struct {
		name  string
		value any
		mode  os.FileMode
		stage AtomicWriteStage
	}{
		{name: "unsupported JSON value", value: math.NaN(), mode: 0600, stage: AtomicWriteMarshal},
		{name: "zero mode", value: atomicWriterFixture{}, mode: 0, stage: AtomicWriteChmod},
		{name: "non-permission mode", value: atomicWriterFixture{}, mode: os.ModeSymlink | 0600, stage: AtomicWriteChmod},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "record.json")
			err := NewAtomicJSONWriter().WriteJSON(target, test.value, test.mode)
			if err == nil {
				t.Fatal("expected error")
			}
			var writeErr *AtomicWriteError
			if !errors.As(err, &writeErr) || writeErr.Stage != test.stage || writeErr.TargetReplaced {
				t.Fatalf("write error = %v, want stage %s before rename", err, test.stage)
			}
			entries, readErr := os.ReadDir(dir)
			if readErr != nil {
				t.Fatalf("read parent directory: %v", readErr)
			}
			if len(entries) != 0 {
				t.Fatalf("unexpected entries after rejected input: %v", entries)
			}
		})
	}
}

func TestAtomicJSONWriterDoesNotCreateParentDirectory(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Join(dir, "missing")
	target := filepath.Join(parent, "record.json")
	err := NewAtomicJSONWriter().WriteJSON(target, atomicWriterFixture{Version: 1}, 0600)
	if err == nil {
		t.Fatal("expected create-temp error for missing parent")
	}
	var writeErr *AtomicWriteError
	if !errors.As(err, &writeErr) || writeErr.Stage != AtomicWriteCreateTemp || writeErr.TargetReplaced {
		t.Fatalf("write error = %v, want create-temp failure before rename", err)
	}
	if _, statErr := os.Stat(parent); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("parent stat error = %v, want not exist", statErr)
	}
}

func TestAtomicJSONWriterConcurrentWritesKeepWholeJSON(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "record.json")
	writer := NewAtomicJSONWriter()
	const writers = 24

	start := make(chan struct{})
	results := make(chan error, writers)
	var group sync.WaitGroup
	for i := 0; i < writers; i++ {
		group.Add(1)
		go func(version int) {
			defer group.Done()
			<-start
			results <- writer.WriteJSON(target, atomicWriterFixture{
				Version: version,
				Payload: strings.Repeat(string(rune('a'+version)), 1024),
			}, 0600)
		}(i)
	}
	close(start)
	group.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("concurrent write failed: %v", err)
		}
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read final target: %v", err)
	}
	var final atomicWriterFixture
	if err := json.Unmarshal(data, &final); err != nil {
		t.Fatalf("final target is incomplete JSON: %v", err)
	}
	if final.Version < 0 || final.Version >= writers {
		t.Fatalf("final version %d is outside the writers", final.Version)
	}
	if final.Payload != strings.Repeat(string(rune('a'+final.Version)), 1024) {
		t.Fatalf("final payload does not match writer %d", final.Version)
	}
	assertOnlyTargetFile(t, dir)
}

func assertAtomicWriteError(t *testing.T, err error, injected error, stage AtomicWriteStage, replaced bool) {
	t.Helper()
	if !errors.Is(err, injected) {
		t.Fatalf("error %v does not wrap injected error %v", err, injected)
	}
	var writeErr *AtomicWriteError
	if !errors.As(err, &writeErr) {
		t.Fatalf("error %v is not AtomicWriteError", err)
	}
	if writeErr.Stage != stage || writeErr.TargetReplaced != replaced {
		t.Fatalf("stage/replaced = %s/%t, want %s/%t", writeErr.Stage, writeErr.TargetReplaced, stage, replaced)
	}
}

func assertAtomicJSON(t *testing.T, path string, want atomicWriterFixture) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	var got atomicWriterFixture
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode target: %v", err)
	}
	if got != want {
		t.Fatalf("target = %+v, want %+v", got, want)
	}
}

func assertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat target: %v", err)
	}
	if info.Mode().Perm() != want {
		t.Fatalf("mode = %04o, want %04o", info.Mode().Perm(), want)
	}
}

func assertOnlyTargetFile(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read parent directory: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "record.json" {
		t.Fatalf("parent entries = %v, want only record.json", entries)
	}
}

func TestAtomicJSONWriterCreateOnlyNeverReplaces(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "record.json")
	writer := NewAtomicJSONWriter()
	if err := writer.WriteJSONNew(target, atomicWriterFixture{Version: 1}, 0600); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteJSONNew(target, atomicWriterFixture{Version: 2}, 0600); !errors.Is(err, os.ErrExist) {
		t.Fatalf("existing target err=%v", err)
	}
	assertAtomicJSON(t, target, atomicWriterFixture{Version: 1})
	assertOnlyTargetFile(t, dir)
}
