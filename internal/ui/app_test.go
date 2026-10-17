package ui

import (
	"errors"
	"image"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

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

func TestStartSyncDoesNotDeadlock(t *testing.T) {
	tempDir := t.TempDir()
	syncer := appSync.New(&mockStore{}, tempDir)
	app := New(syncer, tempDir, "my-bucket", "user", "us-east-1")

	// Trigger sync
	app.startSync()

	// Wait for sync to complete (should be fast with mockStore)
	deadline := time.Now().Add(2 * time.Second)
	for {
		app.mu.Lock()
		syncing := app.isSyncing
		msg := app.statusMsg
		app.mu.Unlock()

		if !syncing {
			if msg != "All saves are up to date!" {
				t.Fatalf("expected status 'All saves are up to date!', got %q", msg)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for sync to finish (possible deadlock)")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSetInitError(t *testing.T) {
	app := New(nil, "/test/dir", "my-bucket", "user", "us-east-1")
	app.SetInitError(errors.New("test error"))

	if !app.statusIsError {
		t.Errorf("expected statusIsError to be true")
	}
	if len(app.logs) == 0 {
		t.Errorf("expected logs to contain error")
	}
}

func TestDrawCardDimensions(t *testing.T) {
	var ops op.Ops
	gtx := layout.Context{
		Ops: &ops,
		Constraints: layout.Constraints{
			Max: image.Point{X: 800, Y: 600},
		},
	}
	dims := drawCard(gtx, colorCard, colorCardBorder, unit.Dp(8), layout.UniformInset(unit.Dp(10)), func(gtx layout.Context) layout.Dimensions {
		return layout.Dimensions{Size: image.Point{X: 200, Y: 30}}
	})
	// Inset is 10 on all sides so height is 30 + 20 = 50. Must not be 600!
	if dims.Size.Y > 60 {
		t.Fatalf("drawCard returned excessive height: %d, expected ~50", dims.Size.Y)
	}
	if dims.Size.X > 230 {
		t.Fatalf("drawCard returned excessive width: %d, expected ~220", dims.Size.X)
	}
}


