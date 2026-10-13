package sync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type mockStore struct {
	files map[string][]byte
}

func newMockStore() *mockStore {
	return &mockStore{files: make(map[string][]byte)}
}

func (m *mockStore) Download(filename string) ([]byte, error) {
	data, ok := m.files[filename]
	if !ok {
		return nil, &s3types.NoSuchKey{Message: nil}
	}
	return data, nil
}

func (m *mockStore) Upload(filename string, data []byte) error {
	m.files[filename] = data
	return nil
}

func TestIsGameDir(t *testing.T) {
	testCases := []struct {
		name     string
		expected bool
	}{
		{"4D5307E6", true},
		{"4d5307e6", true},
		{"00000002", true},
		{"FFFE07D1", false}, // base xbox profile
		{"fffe07d1", false}, // base xbox profile lowercase
		{".git", false},
		{".DS_Store", false},
		{"4D5307E6.bak", false},
		{"4D5307E6-tmp-12345", false},
		{"short", false},
		{"toolongtitleid", false},
		{"4D53ZZZZ", false}, // non-hex
	}

	for _, tc := range testCases {
		got := isGameDir(tc.name)
		if got != tc.expected {
			t.Errorf("isGameDir(%q) = %v, expected %v", tc.name, got, tc.expected)
		}
	}
}

func TestSyncerUploadNewLocalGame(t *testing.T) {
	saveDir := t.TempDir()
	store := newMockStore()

	// Create local game save
	gameID := "4D5307E6"
	gameDir := filepath.Join(saveDir, gameID, "00000001")
	if err := os.MkdirAll(gameDir, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gameDir, "save.dat"), []byte("save content"), 0644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	syncer := New(store, saveDir)
	if err := syncer.Sync(); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	// Verify game was uploaded to store
	if _, ok := store.files[gameID+".zip"]; !ok {
		t.Fatalf("expected %s.zip to be uploaded", gameID)
	}

	// Verify cloud meta.json contains the game
	metaBytes, ok := store.files["meta.json"]
	if !ok {
		t.Fatalf("expected meta.json to be uploaded to cloud")
	}
	var cloudMeta metadata
	if err := json.Unmarshal(metaBytes, &cloudMeta); err != nil {
		t.Fatalf("unmarshal cloud meta failed: %v", err)
	}
	if _, ok := cloudMeta.Games[gameID]; !ok {
		t.Fatalf("expected game %s in cloud metadata", gameID)
	}

	// Run Sync again - should skip without re-uploading
	zipBefore := store.files[gameID+".zip"]
	if err := syncer.Sync(); err != nil {
		t.Fatalf("second Sync failed: %v", err)
	}
	// Object should be identical (no re-upload)
	if string(store.files[gameID+".zip"]) != string(zipBefore) {
		t.Errorf("unexpected re-upload on unchanged game")
	}
}

func TestSyncerDownloadGame(t *testing.T) {
	deviceADir := t.TempDir()
	deviceBDir := t.TempDir()
	store := newMockStore()

	// Device A creates game and syncs
	gameID := "4156004D"
	gameDirA := filepath.Join(deviceADir, gameID, "00000001")
	_ = os.MkdirAll(gameDirA, 0755)
	_ = os.WriteFile(filepath.Join(gameDirA, "save.dat"), []byte("cabela save"), 0644)

	syncerA := New(store, deviceADir)
	if err := syncerA.Sync(); err != nil {
		t.Fatalf("Device A Sync failed: %v", err)
	}

	// Device B syncs (game does not exist on Device B yet)
	syncerB := New(store, deviceBDir)
	if err := syncerB.Sync(); err != nil {
		t.Fatalf("Device B Sync failed: %v", err)
	}

	// Verify Device B downloaded the game
	downloadedFile := filepath.Join(deviceBDir, gameID, "00000001", "save.dat")
	data, err := os.ReadFile(downloadedFile)
	if err != nil {
		t.Fatalf("expected downloaded file on Device B: %v", err)
	}
	if string(data) != "cabela save" {
		t.Fatalf("expected 'cabela save', got %q", string(data))
	}

	// CRITICAL TEST: Running Sync on Device B again must NOT re-upload!
	cloudMetaBefore := store.files["meta.json"]
	if err := syncerB.Sync(); err != nil {
		t.Fatalf("Device B second Sync failed: %v", err)
	}
	if string(store.files["meta.json"]) != string(cloudMetaBefore) {
		t.Errorf("Device B re-uploaded metadata immediately after download (ping-pong bug)!")
	}
}

func TestSyncerAutomaticDownloadWhenCloudUpdated(t *testing.T) {
	deviceADir := t.TempDir()
	deviceBDir := t.TempDir()
	store := newMockStore()

	gameID := "555308CD"

	// 1. Device A creates game and syncs
	gameDirA := filepath.Join(deviceADir, gameID)
	_ = os.MkdirAll(gameDirA, 0755)
	_ = os.WriteFile(filepath.Join(gameDirA, "save.dat"), []byte("version 1"), 0644)
	syncerA := New(store, deviceADir)
	_ = syncerA.Sync()

	// 2. Device B syncs version 1
	syncerB := New(store, deviceBDir)
	_ = syncerB.Sync()

	// 3. Time passes. Device A plays game and writes version 2
	time.Sleep(10 * time.Millisecond)
	_ = os.WriteFile(filepath.Join(gameDirA, "save.dat"), []byte("version 2"), 0644)
	_ = syncerA.Sync()

	// 4. Device B (untouched) syncs. It should AUTOMATICALLY download version 2 without prompt!
	if err := syncerB.Sync(); err != nil {
		t.Fatalf("Device B Sync failed: %v", err)
	}

	fileB := filepath.Join(deviceBDir, gameID, "save.dat")
	contentB, _ := os.ReadFile(fileB)
	if string(contentB) != "version 2" {
		t.Errorf("expected Device B to auto-download 'version 2', got %q", string(contentB))
	}
}
