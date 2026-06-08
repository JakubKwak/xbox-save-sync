# xbox-save-sync

A simple tool for syncing Xenia emulator saves between devices using AWS S3.

Designed to allow sumple cloud saves between PC and Steam Deck.

To build on Windows:
```
go build -o bin\xbox-save-sync.exe .\cmd\savesync
.\bin\xbox-save-sync.exe
```

To build for Linux:
```
$env:GOOS="linux"; $env:GOARCH="amd64"; go build -o bin/xbox-save-sync-linux ./cmd/savesync
```
Then add as non-Steam game.