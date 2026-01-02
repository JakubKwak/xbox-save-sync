package main

import (
	"fmt"
	"log"
	"os"
	"xbox-save-sync/internal/s3"
	"xbox-save-sync/internal/sync"

	"github.com/joho/godotenv"
)

func main() {
	err := godotenv.Load(".env")
	if err != nil {
		log.Println("No .env file found, using environment variables")
	}

	saveDir := os.Getenv("SAVE_DIR")
	prefix := os.Getenv("CLOUD_PREFIX")

	bucket := os.Getenv("AWS_S3_BUCKET")
	region := os.Getenv("AWS_REGION")

	// Should be loaded automatically by aws/config
	//accessKeyId := os.Getenv("AWS_ACCESS_KEY_ID")
	//secretKet := os.Getenv("AWS_SECRET_ACCESS_KEY")

	store, err := s3.New(bucket, prefix, region)
	if err != nil {
		fmt.Printf("Error loading S3 Client: %s\n", err.Error())
		return
	}

	syncer := sync.New(store, saveDir)
	if err = syncer.Sync(); err != nil {
		fmt.Printf("Error Syncing: %s\n", err.Error())
		return
	}
}
