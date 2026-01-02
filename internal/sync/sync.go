package sync

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const metaFileName string = "meta.json"

type store interface {
	Download(filename string) ([]byte, error)
	Upload(filename string, data []byte) error
}

type Syncer struct {
	store    store
	savePath string
}

type metadata struct {
	games map[string]time.Time
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

	// Sync games already in cloud
	for game, cloudVer := range metaCloud.games {
		var localVer *time.Time
		if ver, ok := metaLocal.games[game]; ok {
			localVer = &ver
		}
		newVer, err := s.syncGame(game, &cloudVer, localVer)
		if err != nil {
			return err
		}

		if newVer != nil {
			metaCloud.games[game] = *newVer
			metaLocal.games[game] = *newVer
		}

		// Sync metadata between each operation in case of error
		if err := s.writeMetaCloud(metaCloud); err != nil {
			return err
		}
		if err := s.writeMetaLocal(metaLocal); err != nil {
			return err
		}
	}

	// Sync local games not in cloud
	entires, err := os.ReadDir(s.savePath)
	if err != nil {
		return err
	}
	for _, entry := range entires {
		if !entry.IsDir() {
			continue
		}
		game := entry.Name()

		if _, ok := metaCloud.games[game]; ok {
			continue // already exists in cloud
		}

		ver := time.Now()
		newVer, err := s.syncGame(game, nil, &ver)
		if err != nil {
			return err
		}
		if newVer != nil {
			metaCloud.games[game] = *newVer
			metaLocal.games[game] = *newVer
		}
		if err := s.writeMetaCloud(metaCloud); err != nil {
			return err
		}
		if err := s.writeMetaLocal(metaLocal); err != nil {
			return err
		}
	}

	return nil
}

func (s *Syncer) syncGame(game string, cloudVer *time.Time, localVer *time.Time) (*time.Time, error) {
	fmt.Println("Syncing game " + game)

	if cloudVer == nil && localVer == nil {
		return nil, errors.New("both cloud and local saves lack version")
	}

	if localVer == nil {
		if err := s.downloadGame(game); err != nil {
			return nil, err
		}

		return cloudVer, nil
	}

	if cloudVer == nil {
		if err := s.uploadGame(game); err != nil {
			return nil, err
		}

		ver := time.Now()
		return &ver, nil
	}

	if cloudVer != localVer {
		reader := bufio.NewReader(os.Stdin)
		fmt.Printf("Version mismatch - Local: %s, Cloud: %s\n", localVer.Format(time.RFC1123), cloudVer.Format(time.RFC1123))
		fmt.Println("Download (Y) - Upload (X) - Cancel (B)")

		for {
			text, _ := reader.ReadString('\n')
			switch text {
			case "Y":
				if err := s.downloadGame(game); err != nil {
					return nil, err
				}

				return cloudVer, nil
			case "X":
				if err := s.uploadGame(game); err != nil {
					return nil, err
				}
				ver := time.Now()
				return &ver, nil
			case "B":
				return nil, nil
			default:
				fmt.Printf("Unknown input: %s", text)
			}
		}
	}

	return nil, nil
}

func (s *Syncer) uploadGame(game string) error {
	fmt.Println("Uploading...")

	saveDir := filepath.Join(s.savePath, metaFileName)

	data, err := zipDir(saveDir)
	if err != nil {
		return err
	}

	fmt.Println("Done.")

	return s.store.Upload(game+".zip", data)
}

func (s *Syncer) downloadGame(game string) error {
	fmt.Println("Downloading...")

	zipData, err := s.store.Download(game + ".zip")
	if err != nil {
		return err
	}

	targetDir := filepath.Join(s.savePath, game)

	// We extract to a temporary folder first to avoid corrupting local saves if sync breaks midway
	tmpDir, err := os.MkdirTemp(s.savePath, game+"-tmp-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	if err := unzip(zipData, tmpDir); err != nil {
		return err
	}

	backupDir := targetDir + ".bak"
	_ = os.RemoveAll(backupDir)

	// Move existing save to backup dir
	if _, err := os.Stat(targetDir); err == nil {
		if err := os.Rename(targetDir, backupDir); err != nil {
			return err
		}
	}

	// Move new save into place
	if err := os.Rename(tmpDir, targetDir); err != nil {
		_ = os.Rename(backupDir, targetDir)
		return err
	}

	_ = os.RemoveAll(backupDir)

	fmt.Println("Done.")
	return nil
}

func (s *Syncer) readMetaLocal() (metadata, error) {
	file, err := os.Open(filepath.Join(s.savePath, metaFileName))
	if err != nil {
		return metadata{games: map[string]time.Time{}}, err
	}

	data, err := io.ReadAll(file)
	if err != nil {
		return metadata{games: map[string]time.Time{}}, err
	}

	meta := metadata{games: map[string]time.Time{}}
	err = json.Unmarshal(data, &meta)

	return meta, err
}

func (s *Syncer) readMetaCloud() (metadata, error) {
	data, err := s.store.Download(metaFileName)
	if err != nil {
		return metadata{}, err
	}

	var meta metadata
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
