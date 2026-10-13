package ui

import (
	"testing"
	"time"

	appSync "xbox-save-sync/internal/sync"
)

type mockStore struct{}

func (m *mockStore) Download(filename string) ([]byte, error) {
	return nil, nil
}
func (m *mockStore) Upload(filename string, data []byte) error {
	return nil
}

func TestUINew(t *testing.T) {
	syncer := appSync.New(&mockStore{}, "/test/dir")
	app := New(syncer, "/test/dir", "my-bucket", "user", "us-east-1")

	if app == nil {
		t.Fatalf("expected non-nil app")
	}
	if !app.configOK {
		t.Errorf("expected configOK to be true")
	}

	// Test adding logs
	app.addLog("Test log line")
	if len(app.logs) != 1 {
		t.Errorf("expected 1 log line, got %d", len(app.logs))
	}

	// Test conflict channel handling in goroutine
	done := make(chan struct{})
	go func() {
		choice, err := app.promptConflict(appSync.ConflictInfo{
			GameID:    "4D5307E6",
			Title:     "Halo 3",
			LocalTime: time.Now(),
			CloudTime: time.Now(),
		})
		if err != nil {
			t.Errorf("promptConflict error: %v", err)
		}
		if choice != appSync.ChoiceDownload {
			t.Errorf("expected ChoiceDownload, got %v", choice)
		}
		close(done)
	}()

	// Simulate user tapping Download in the UI
	time.Sleep(20 * time.Millisecond)
	app.conflictChan <- appSync.ChoiceDownload

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for conflict resolution")
	}
}

