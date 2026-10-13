package sync

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"xbox-save-sync/internal/mapping"

	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

const metaFileName string = "meta.json"

const baseGameId string = "FFFE07D1"

type store interface {
	Download(filename string) ([]byte, error)
	Upload(filename string, data []byte) error
}

type ConflictChoice int

const (
	ChoiceDownload ConflictChoice = iota
	ChoiceUpload
	ChoiceSkip
)

type ConflictInfo struct {
	GameID    string
	Title     string
	LocalTime time.Time
	CloudTime time.Time
}

type ConflictResolver func(info ConflictInfo) (ConflictChoice, error)

type Logger func(format string, args ...any)

type ProgressCallback func(gameTitle string, action string)

type Syncer struct {
	store            store
	savePath         string
	logFn            Logger
	conflictResolver ConflictResolver
	progressFn       ProgressCallback
}

type metadata struct {
	Games map[string]time.Time `json:"games"`
}

func New(store store, savePath string) *Syncer {
	return &Syncer{store: store, savePath: savePath}
}

func (s *Syncer) SetLogger(l Logger) {
	s.logFn = l
}

func (s *Syncer) SetConflictResolver(r ConflictResolver) {
	s.conflictResolver = r
}

func (s *Syncer) SetProgressCallback(p ProgressCallback) {
	s.progressFn = p
}

func (s *Syncer) log(format string, args ...any) {
	if s.logFn != nil {
		s.logFn(format, args...)
	} else {
		fmt.Printf(format+"\n", args...)
	}
}

func (s *Syncer) progress(gameTitle string, action string) {
	if s.progressFn != nil {
		s.progressFn(gameTitle, action)
	}
}

func isGameDir(name string) bool {
	if strings.EqualFold(name, baseGameId) {
		return false
	}
	if strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".bak") || strings.Contains(name, "-tmp-") {
		return false
	}
	// Xenia title IDs are 8-character hexadecimal strings
	if len(name) != 8 {
		return false
	}
	for _, c := range name {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func (s *Syncer) cleanStaleArtifacts() {
	entries, err := os.ReadDir(s.savePath)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.Contains(name, "-tmp-") || strings.HasSuffix(name, ".bak") {
			_ = os.RemoveAll(filepath.Join(s.savePath, name))
		}
	}
}

func (s *Syncer) Sync() error {
	if err := os.MkdirAll(s.savePath, 0755); err != nil {
		return fmt.Errorf("ensuring save directory: %w", err)
	}

	s.cleanStaleArtifacts()

	metaLocal, err := s.readMetaLocal()
	if err != nil {
		return err
	}

	metaCloud, err := s.readMetaCloud()
	if err != nil {
		return err
	}

	// Read local folders - if any game does not exist in the local meta.json add it
	entries, err := os.ReadDir(s.savePath)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		game := entry.Name()
		if !isGameDir(game) {
			continue
		}
		if _, ok := metaLocal.Games[game]; !ok {
			metaLocal.Games[game] = time.Time{}
		}
	}

	existingErr := s.syncExisting(&metaCloud, &metaLocal)
	newErr := s.syncNew(&metaCloud, &metaLocal)

	if existingErr != nil {
		return existingErr
	}
	return newErr
}

func (s *Syncer) syncExisting(metaCloud, metaLocal *metadata) error {
	var errs []string
	for game, cloudVer := range metaCloud.Games {
		if !isGameDir(game) {
			continue
		}

		var localVer *time.Time
		if ver, ok := metaLocal.Games[game]; ok && !ver.IsZero() {
			localVer = &ver
		}
		newVer, err := s.syncGame(game, &cloudVer, localVer)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %s", game, err.Error()))
			fmt.Printf("Error syncing %s: %s\n", mapping.Title(game), err)
			continue
		}

		if newVer != nil {
			metaCloud.Games[game] = *newVer
			metaLocal.Games[game] = *newVer

			if err := s.writeMetaCloud(*metaCloud); err != nil {
				return fmt.Errorf("saving cloud metadata: %w", err)
			}
			if err := s.writeMetaLocal(*metaLocal); err != nil {
				return fmt.Errorf("saving local metadata: %w", err)
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed syncing games: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (s *Syncer) syncNew(metaCloud, metaLocal *metadata) error {
	var errs []string
	for game, localVer := range metaLocal.Games {
		if !isGameDir(game) {
			continue
		}
		if _, ok := metaCloud.Games[game]; ok {
			continue // If already exists in cloud saves, skip it
		}
		var lVer *time.Time
		if !localVer.IsZero() {
			lVer = &localVer
		}
		newVer, err := s.syncGame(game, nil, lVer)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %s", game, err.Error()))
			fmt.Printf("Error syncing %s: %s\n", mapping.Title(game), err)
			continue
		}

		if newVer != nil {
			metaCloud.Games[game] = *newVer
			metaLocal.Games[game] = *newVer

			if err := s.writeMetaCloud(*metaCloud); err != nil {
				return fmt.Errorf("saving cloud metadata: %w", err)
			}
			if err := s.writeMetaLocal(*metaLocal); err != nil {
				return fmt.Errorf("saving local metadata: %w", err)
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed syncing games: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (s *Syncer) syncGame(game string, cloudVer *time.Time, localVer *time.Time) (*time.Time, error) {
	title := mapping.Title(game)
	s.log("Syncing game %s", title)
	s.progress(title, "Checking...")

	saveDir := filepath.Join(s.savePath, game)
	latestLocalWrite, err := getLatestModTime(saveDir)
	hasLocalFiles := err == nil && !latestLocalWrite.IsZero()
	hasCloud := cloudVer != nil && !cloudVer.IsZero()
	hasLocalVer := localVer != nil && !localVer.IsZero()

	// Case 1: Game exists in cloud, but no local files or no prior local sync
	if hasCloud && (!hasLocalFiles || !hasLocalVer) {
		if hasLocalFiles {
			// Both exist, but this device hasn't synced with cloud before. Prompt conflict resolution.
			return s.resolveConflict(game, title, latestLocalWrite, *cloudVer)
		}
		s.progress(title, "Downloading...")
		if err := s.downloadGame(game, cloudVer); err != nil {
			return nil, err
		}
		s.progress(title, "Downloaded")
		return cloudVer, nil
	}

	// Case 2: Game exists only locally (new game, no cloud save yet)
	if !hasCloud && hasLocalFiles {
		s.progress(title, "Uploading...")
		if err := s.uploadGame(game); err != nil {
			return nil, err
		}
		ver := time.Now().UTC()
		s.progress(title, "Uploaded")
		return &ver, nil
	}

	// Case 3: Neither cloud nor local has save files
	if !hasCloud && !hasLocalFiles {
		return nil, nil
	}

	// Case 4: Both exist and were previously synced
	localModified := latestLocalWrite.After(*localVer)
	cloudChanged := !cloudVer.Equal(*localVer)

	if !localModified && !cloudChanged {
		s.log("No change detected for %s. Skipping.", title)
		s.progress(title, "Up to date")
		return nil, nil
	}

	if localModified && !cloudChanged {
		s.progress(title, "Uploading...")
		if err := s.uploadGame(game); err != nil {
			return nil, err
		}
		ver := time.Now().UTC()
		s.progress(title, "Uploaded")
		return &ver, nil
	}

	if !localModified && cloudChanged {
		s.progress(title, "Downloading...")
		if err := s.downloadGame(game, cloudVer); err != nil {
			return nil, err
		}
		s.progress(title, "Downloaded")
		return cloudVer, nil
	}

	// True conflict: both modified since last sync
	return s.resolveConflict(game, title, latestLocalWrite, *cloudVer)
}

func (s *Syncer) resolveConflict(game, title string, localTime, cloudTime time.Time) (*time.Time, error) {
	if s.conflictResolver != nil {
		choice, err := s.conflictResolver(ConflictInfo{
			GameID:    game,
			Title:     title,
			LocalTime: localTime,
			CloudTime: cloudTime,
		})
		if err != nil {
			return nil, err
		}
		switch choice {
		case ChoiceDownload:
			s.progress(title, "Downloading...")
			if err := s.downloadGame(game, &cloudTime); err != nil {
				return nil, err
			}
			s.progress(title, "Downloaded")
			return &cloudTime, nil
		case ChoiceUpload:
			s.progress(title, "Uploading...")
			if err := s.uploadGame(game); err != nil {
				return nil, err
			}
			ver := time.Now().UTC()
			s.progress(title, "Uploaded")
			return &ver, nil
		case ChoiceSkip:
			s.log("Skipped %s.", title)
			s.progress(title, "Skipped")
			return nil, nil
		}
	}
	return s.promptResolveConflict(game, title, localTime, cloudTime)
}

func (s *Syncer) promptResolveConflict(game, title string, localTime, cloudTime time.Time) (*time.Time, error) {
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("\nConflict detected for %s:\n", title)
	fmt.Printf("  Local save modified: %s\n", localTime.Local().Format(time.RFC1123))
	fmt.Printf("  Cloud save uploaded: %s\n", cloudTime.Local().Format(time.RFC1123))
	fmt.Println("  Download (Y/D) - Upload (X/U) - Cancel (B/C)")

	for {
		fmt.Print("Choice (Y/X/B): ")
		text, err := reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("reading input: %w", err)
		}
		text = strings.TrimSpace(strings.ToUpper(text))
		if len(text) == 0 {
			continue
		}
		character := text[0]
		switch character {
		case 'Y', 'D':
			if err := s.downloadGame(game, &cloudTime); err != nil {
				return nil, err
			}
			return &cloudTime, nil
		case 'X', 'U':
			if err := s.uploadGame(game); err != nil {
				return nil, err
			}
			ver := time.Now().UTC()
			return &ver, nil
		case 'B', 'C', 'Q':
			fmt.Println("Skipped.")
			return nil, nil
		default:
			fmt.Printf("Unknown input: %s\n", text)
		}
	}
}

func (s *Syncer) uploadGame(game string) error {
	title := mapping.Title(game)
	s.log("Uploading %s...", title)

	saveDir := filepath.Join(s.savePath, game)

	data, err := zipDir(saveDir)
	if err != nil {
		return fmt.Errorf("cannot zip save: %w", err)
	}

	if err := s.store.Upload(game+".zip", data); err != nil {
		return fmt.Errorf("cannot upload save: %w", err)
	}

	s.log("Uploaded %s successfully.", title)
	return nil
}

func (s *Syncer) downloadGame(game string, cloudVer *time.Time) error {
	title := mapping.Title(game)
	s.log("Downloading %s...", title)

	zipData, err := s.store.Download(game + ".zip")
	if err != nil {
		return fmt.Errorf("cannot download save: %w", err)
	}

	targetDir := filepath.Join(s.savePath, game)

	// Clean up any stale temp directories matching this game
	tmpPrefix := game + "-tmp-"
	if entries, err := os.ReadDir(s.savePath); err == nil {
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), tmpPrefix) {
				_ = os.RemoveAll(filepath.Join(s.savePath, entry.Name()))
			}
		}
	}

	// We extract to a temporary folder first to avoid corrupting local saves if sync breaks midway
	tmpDir, err := os.MkdirTemp(s.savePath, tmpPrefix)
	if err != nil {
		return fmt.Errorf("cannot make temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := unzip(zipData, tmpDir); err != nil {
		return fmt.Errorf("cannot unzip save: %w", err)
	}

	backupDir := targetDir + ".bak"
	_ = os.RemoveAll(backupDir)

	// Move existing save to backup dir if it exists
	if _, err := os.Stat(targetDir); err == nil {
		if err := os.Rename(targetDir, backupDir); err != nil {
			return fmt.Errorf("cannot backup save: %w", err)
		}
	}

	// Move new save into place
	if err := os.Rename(tmpDir, targetDir); err != nil {
		_ = os.Rename(backupDir, targetDir)
		return fmt.Errorf("cannot move new save: %w", err)
	}

	// Clamp extracted files' mtime to cloudVer to prevent false upload triggers
	if cloudVer != nil && !cloudVer.IsZero() {
		_ = clampModTime(targetDir, *cloudVer)
	}

	_ = os.RemoveAll(backupDir)

	s.log("Downloaded %s successfully.", title)
	return nil
}

func clampModTime(dirPath string, maxTime time.Time) error {
	return filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && info.ModTime().After(maxTime) {
			_ = os.Chtimes(path, maxTime, maxTime)
		}
		return nil
	})
}

func (s *Syncer) readMetaLocal() (metadata, error) {
	meta := metadata{Games: make(map[string]time.Time)}
	data, err := os.ReadFile(filepath.Join(s.savePath, metaFileName))
	if err != nil {
		if os.IsNotExist(err) {
			s.log("Could not find local metadata file, creating new one")
			return meta, nil
		}
		return meta, err
	}

	if len(data) == 0 {
		return meta, nil
	}

	if err := json.Unmarshal(data, &meta); err != nil {
		return meta, err
	}
	if meta.Games == nil {
		meta.Games = make(map[string]time.Time)
	}

	return meta, nil
}

func (s *Syncer) readMetaCloud() (metadata, error) {
	meta := metadata{Games: make(map[string]time.Time)}
	data, err := s.store.Download(metaFileName)
	if err != nil {
		var noSuchKey *s3types.NoSuchKey
		var apiErr smithy.APIError
		if errors.As(err, &noSuchKey) || (errors.As(err, &apiErr) && (apiErr.ErrorCode() == "NoSuchKey" || apiErr.ErrorCode() == "NotFound")) {
			s.log("Metadata does not exist yet: %s", metaFileName)
			return meta, nil
		}

		return meta, err
	}

	if len(data) == 0 {
		return meta, nil
	}

	if err := json.Unmarshal(data, &meta); err != nil {
		return meta, err
	}
	if meta.Games == nil {
		meta.Games = make(map[string]time.Time)
	}

	return meta, nil
}

func (s *Syncer) writeMetaCloud(meta metadata) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}

	return s.store.Upload(metaFileName, data)
}

func (s *Syncer) writeMetaLocal(meta metadata) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}

	targetPath := filepath.Join(s.savePath, metaFileName)
	tmpPath := targetPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, targetPath)
}

func getLatestModTime(dirPath string) (time.Time, error) {
	var latest time.Time

	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			if info.ModTime().After(latest) {
				latest = info.ModTime()
			}
		}
		return nil
	})

	return latest, err
}
