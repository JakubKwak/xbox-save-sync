# xbox-save-sync

A simple tool for syncing Xenia emulator saves between devices using AWS S3.

Designed to allow simple cloud saves between PC and Steam Deck with a native graphical UI built with [Gio](https://gioui.org).

### Features
- **Steam Deck & Touchscreen Friendly:** Large, touch-friendly buttons for syncing and conflict resolution—no on-screen keyboard or controller button remapping required.
- **Automatic Smart Sync:** Downloads newer cloud saves and uploads modified local saves automatically.
- **Interactive Conflict Resolver:** If a save was modified on both devices independently, presents large touch buttons to choose whether to keep cloud or local.
- **Compressed & Efficient:** Save archives are compressed with ZIP Deflate to reduce bandwidth and S3 storage.
- **Preserves Timestamps:** Restores original file modification timestamps to prevent false upload loops.
- **CLI Mode Available:** Run with `--cli` for automated or headless terminal execution.

### Setup
Create a `.env` file in the application directory (or next to the binary):
```env
AWS_ACCESS_KEY_ID=AKIA...
AWS_SECRET_ACCESS_KEY=...
AWS_S3_BUCKET=my-xenia-saves
AWS_REGION=eu-west-2
SAVE_DIR=/home/deck/.var/app/org.xenia.xenia_canary/data/xenia/content
CLOUD_PREFIX=user
```

### Building

**Windows:**
```cmd
go build -o bin\xbox-save-sync.exe .\cmd\savesync
.\bin\xbox-save-sync.exe
```

**Linux / Steam Deck (native):**
On Linux or directly in Steam Deck Desktop Mode:
```bash
go build -o bin/xbox-save-sync-linux ./cmd/savesync
```
Then add `bin/xbox-save-sync-linux` as a Non-Steam Game in Steam.

**Headless / Terminal Mode:**
```bash
./xbox-save-sync --cli
```