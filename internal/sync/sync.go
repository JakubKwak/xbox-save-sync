package sync

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"xbox-save-sync/internal/mapping"

	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	"time"
)

const metaFileName string = "meta.json"

const baseGameId string = "FFFE07D1"

type store interface {
	Download(filename string) ([]byte, error)
	Upload(filename string, data []byte) error
}

type Syncer struct {
	store    store
	savePath string
}

type metadata struct {
	Games map[string]time.Time `json:"games"`
}

func New(store store, savePath string) *Syncer {
	return &Syncer{store: store, savePath: savePath}
}

func (s *Syncer) Sync() error {
	metaLocal, err := s.readMetaLocal()
	if err != nil {
		return err
	}

	metaCloud, err := s.readMetaCloud()
	if err != nil {
		return err
	}

	// Read local folders - if any game does not exist in the local meta.json add it
	entires, err := os.ReadDir(s.savePath)
	if err != nil {
		return err
	}
	for _, entry := range entires {
		if !entry.IsDir() {
			continue
		}
		game := entry.Name()
		if game == baseGameId {
			// base xbox profile game id, reserved for accounts info, skip
			continue
		}
		if _, ok := metaLocal.Games[game]; !ok {
			metaLocal.Games[game] = time.Time{}
		}
	}

	if err := s.syncExisting(&metaCloud, &metaLocal); err != nil {
		return err
	}
	if err := s.syncNew(&metaCloud, &metaLocal); err != nil {
		return err
	}

	return nil
}

func (s *Syncer) syncExisting(metaCloud, metaLocal *metadata) error {
	for game, cloudVer := range metaCloud.Games {
		if game == baseGameId {
			// base xbox profile game id, reserved for accounts info, skip
			continue
		}

		var localVer *time.Time
		if ver, ok := metaLocal.Games[game]; ok {
			localVer = &ver
		}
		newVer, err := s.syncGame(game, &cloudVer, localVer)
		if err != nil {
			return err
		}

		if newVer != nil {
			metaCloud.Games[game] = *newVer
			metaLocal.Games[game] = *newVer
		}

		// Sync metadata between each operation in case of error
		if err := s.writeMetaCloud(*metaCloud); err != nil {
			return err
		}
		if err := s.writeMetaLocal(*metaLocal); err != nil {
			return err
		}
	}

	return nil
}

func (s *Syncer) syncNew(metaCloud, metaLocal *metadata) error {
	for game, localVer := range metaLocal.Games {
		if _, ok := metaCloud.Games[game]; ok {
			continue // If already exists in cloud saves, skip it
		}
		newVer, err := s.syncGame(game, nil, &localVer)
		if err != nil {
			return err
		}

		if newVer != nil {
			metaCloud.Games[game] = *newVer
			metaLocal.Games[game] = *newVer
		}

		// Sync metadata between each operation in case of error
		if err := s.writeMetaCloud(*metaCloud); err != nil {
			return err
		}
		if err := s.writeMetaLocal(*metaLocal); err != nil {
			return err
		}
	}

	return nil
}

func (s *Syncer) syncGame(game string, cloudVer *time.Time, localVer *time.Time) (*time.Time, error) {
	fmt.Println("Syncing game " + mapping.Title(game))

	if cloudVer == nil && localVer == nil {
		return nil, errors.New("both cloud and local saves lack version")
	}

	if localVer == nil {
		if err := s.downloadGame(game); err != nil {
			return nil, err
		}

		return cloudVer, nil
	}

	if cloudVer == nil || cloudVer.Format(time.RFC3339) == localVer.Format(time.RFC3339) {
		if err := s.uploadGame(game); err != nil {
			return nil, err
		}

		ver := time.Now()
		return &ver, nil
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("Version mismatch - Local: %s, Cloud: %s\n", localVer.Format(time.RFC1123), cloudVer.Format(time.RFC1123))
	fmt.Println("Download (Y) - Upload (X) - Cancel (B)")

	for {
		text, _ := reader.ReadString('\n')
		text = strings.ToUpper(text)
		character := text[0]
		switch character {
		case 'Y':
			if err := s.downloadGame(game); err != nil {
				return nil, err
			}

			return cloudVer, nil
		case 'X':
			if err := s.uploadGame(game); err != nil {
				return nil, err
			}
			ver := time.Now()
			return &ver, nil
		case 'B':
			return nil, nil
		default:
			fmt.Printf("Unknown input: %s", text)
		}
	}
}

func (s *Syncer) uploadGame(game string) error {
	fmt.Println("Uploading...")

	saveDir := filepath.Join(s.savePath, game)

	data, err := zipDir(saveDir)
	if err != nil {
		return fmt.Errorf("cannot zip save: %w", err)
	}

	fmt.Println("Done.")

	return s.store.Upload(game+".zip", data)
}

func (s *Syncer) downloadGame(game string) error {
	fmt.Println("Downloading...")

	zipData, err := s.store.Download(game + ".zip")
	if err != nil {
		return fmt.Errorf("cannot download save: %w", err)
	}

	targetDir := filepath.Join(s.savePath, game)

	// We extract to a temporary folder first to avoid corrupting local saves if sync breaks midway
	tmpDir, err := os.MkdirTemp(s.savePath, game+"-tmp-")
	if err != nil {
		return fmt.Errorf("cannot make temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := unzip(zipData, tmpDir); err != nil {
		return fmt.Errorf("cannot unzip save: %w", err)
	}

	backupDir := targetDir + ".bak"
	_ = os.RemoveAll(backupDir)

	// Move existing save to backup dir
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

	_ = os.RemoveAll(backupDir)

	fmt.Println("Done.")
	return nil
}

func (s *Syncer) readMetaLocal() (metadata, error) {
	file, err := os.Open(filepath.Join(s.savePath, metaFileName))
	if err != nil {
		fmt.Println("Could not find/open metadata file, creating new one")
		return metadata{Games: map[string]time.Time{}}, nil
	}

	data, err := io.ReadAll(file)
	if err != nil {
		return metadata{Games: map[string]time.Time{}}, err
	}

	meta := metadata{Games: map[string]time.Time{}}
	err = json.Unmarshal(data, &meta)

	return meta, err
}

func (s *Syncer) readMetaCloud() (metadata, error) {
	data, err := s.store.Download(metaFileName)
	if err != nil {
		var noSuchKey *s3types.NoSuchKey // todo not generic
		if errors.As(err, &noSuchKey) {
			fmt.Println("Metadata does not exist yet:", metaFileName)
			return metadata{Games: map[string]time.Time{}}, nil
		}

		return metadata{Games: map[string]time.Time{}}, err
	}

	meta := metadata{Games: map[string]time.Time{}}
	err = json.Unmarshal(data, &meta)

	return meta, err
}

func (s *Syncer) writeMetaCloud(meta metadata) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}

	return s.store.Upload(metaFileName, data)
}

func (s *Syncer) writeMetaLocal(meta metadata) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(s.savePath, metaFileName), data, 0666)
}
