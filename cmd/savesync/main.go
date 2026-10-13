package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	gioApp "gioui.org/app"
	"xbox-save-sync/internal/s3"
	"xbox-save-sync/internal/sync"
	"xbox-save-sync/internal/ui"

	"github.com/joho/godotenv"
)

func main() {
	cliFlag := flag.Bool("cli", false, "Run in terminal / CLI mode without graphical UI")
	flag.Parse()

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

	var syncer *sync.Syncer
	if bucket != "" && saveDir != "" {
		store, err := s3.New(bucket, prefix, region)
		if err != nil {
			log.Printf("Error creating S3 Store: %v\n", err)
		} else {
			syncer = sync.New(store, saveDir)
		}
	}

	if *cliFlag {
		if bucket == "" {
			fmt.Fprintln(os.Stderr, "Error: AWS_S3_BUCKET environment variable is not set")
			os.Exit(1)
		}
		if saveDir == "" {
			fmt.Fprintln(os.Stderr, "Error: SAVE_DIR environment variable is not set")
			os.Exit(1)
		}
		if syncer == nil {
			fmt.Fprintln(os.Stderr, "Error initializing syncer")
			os.Exit(1)
		}

		if err := syncer.Sync(); err != nil {
			fmt.Fprintf(os.Stderr, "Error Syncing: %s\n", err.Error())
			os.Exit(1)
		}
		return
	}

	// Default: Launch Gio graphical UI
	gui := ui.New(syncer, saveDir, bucket, prefix, region)
	go func() {
		if err := gui.Run(); err != nil {
			log.Printf("UI Error: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}()

	gioApp.Main()
}
