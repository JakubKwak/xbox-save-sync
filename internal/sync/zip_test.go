package sync

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestZipAndUnzipWithTimestampsAndCompression(t *testing.T) {
	tempSrc := t.TempDir()
	tempDst := t.TempDir()

	subDir := filepath.Join(tempSrc, "savedata")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	testFile := filepath.Join(subDir, "save.bin")
	content := bytes.Repeat([]byte("XBOX_SAVE_GAME_DATA_TEST_PATTERN_COMPRESSIBLE"), 1000)
	if err := os.WriteFile(testFile, content, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Set a specific mod time in the past
	expectedTime := time.Date(2025, 5, 10, 12, 30, 0, 0, time.UTC)
	if err := os.Chtimes(testFile, expectedTime, expectedTime); err != nil {
		t.Fatalf("failed to set mod time: %v", err)
	}

	// Zip directory
	zipData, err := zipDir(tempSrc)
	if err != nil {
		t.Fatalf("zipDir failed: %v", err)
	}

	// Verify compression ratio: content was ~45KB, compressed should be much smaller
	if len(zipData) >= len(content)/2 {
		t.Errorf("expected compression to reduce size significantly, original %d, zip %d", len(content), len(zipData))
	}

	// Verify header method is Deflate
	r, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		t.Fatalf("failed to read zip: %v", err)
	}
	if len(r.File) == 0 {
		t.Fatalf("expected at least 1 file in zip")
	}
	if r.File[0].Method != zip.Deflate {
		t.Errorf("expected method Deflate (%d), got %d", zip.Deflate, r.File[0].Method)
	}

	// Unzip into destination
	if err := unzip(zipData, tempDst); err != nil {
		t.Fatalf("unzip failed: %v", err)
	}

	// Verify extracted file content and timestamp
	extractedFile := filepath.Join(tempDst, "savedata", "save.bin")
	extractedContent, err := os.ReadFile(extractedFile)
	if err != nil {
		t.Fatalf("failed to read extracted file: %v", err)
	}
	if !bytes.Equal(extractedContent, content) {
		t.Errorf("extracted content does not match original")
	}

	stat, err := os.Stat(extractedFile)
	if err != nil {
		t.Fatalf("failed to stat extracted file: %v", err)
	}

	// Check that timestamp was preserved within 2 seconds (zip timestamp precision is 2 seconds)
	diff := stat.ModTime().Sub(expectedTime)
	if diff < -2*time.Second || diff > 2*time.Second {
		t.Errorf("expected mod time %v, got %v (diff: %v)", expectedTime, stat.ModTime(), diff)
	}
}

func TestUnzipIllegalPath(t *testing.T) {
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)

	// Create an entry attempting path traversal
	w, err := zw.Create("../outside.txt")
	if err != nil {
		t.Fatalf("failed to create zip entry: %v", err)
	}
	_, _ = w.Write([]byte("malicious"))
	_ = zw.Close()

	destDir := t.TempDir()
	err = unzip(buf.Bytes(), destDir)
	if err == nil {
		t.Errorf("expected illegal file path error for zip-slip, got nil")
	}
}
