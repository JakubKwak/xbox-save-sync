package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"xbox-save-sync/internal/s3"
	"xbox-save-sync/internal/sync"

	"github.com/joho/godotenv"
)

func main() {
	// Try loading .env from current working directory first, then fallback to executable directory
	if err := godotenv.Load(".env"); err != nil {
		if exePath, err := os.Executable(); err == nil {
			exeEnv := filepath.Join(filepath.Dir(exePath), ".env")
			if err := godotenv.Load(exeEnv); err == nil {
				log.Printf("Loaded .env from %s\n", exeEnv)
			} else {
				log.Println("No .env file found, using environment variables")
			}
		} else {
			log.Println("No .env file found, using environment variables")
		}
	}

	saveDir := os.Getenv("SAVE_DIR")
	prefix := os.Getenv("CLOUD_PREFIX")

	bucket := os.Getenv("AWS_S3_BUCKET")
	region := os.Getenv("AWS_REGION")

	// Validate required configuration
	if bucket == "" {
		fmt.Fprintln(os.Stderr, "Error: AWS_S3_BUCKET environment variable is not set")
		os.Exit(1)
	}
	if saveDir == "" {
		fmt.Fprintln(os.Stderr, "Error: SAVE_DIR environment variable is not set")
		os.Exit(1)
	}

	// Expand ~ to user home directory on Linux / Steam Deck / macOS
	if strings.HasPrefix(saveDir, "~/") || saveDir == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			if saveDir == "~" {
				saveDir = home
			} else {
				saveDir = filepath.Join(home, saveDir[2:])
			}
		}
	}

	// Should be loaded automatically by aws/config
	//accessKeyId := os.Getenv("AWS_ACCESS_KEY_ID")
	//secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")

	store, err := s3.New(bucket, prefix, region)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading S3 Client: %s\n", err.Error())
		os.Exit(1)
	}

	syncer := sync.New(store, saveDir)
	if err = syncer.Sync(); err != nil {
		fmt.Fprintf(os.Stderr, "Error Syncing: %s\n", err.Error())
		os.Exit(1)
	}
}
