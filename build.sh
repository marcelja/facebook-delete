GOOS=linux GOARCH=amd64 go build -trimpath -o deleter-linux
GOOS=linux GOARCH=arm64 go build -trimpath -o deleter-linux-arm64
GOOS=darwin GOARCH=amd64 go build -trimpath -o deleter-macos
GOOS=darwin GOARCH=arm64 go build -trimpath -o deleter-macos-arm64
GOOS=windows GOARCH=amd64 go build -trimpath -o deleter-windows.exe
GOOS=windows GOARCH=arm64 go build -trimpath -o deleter-windows-arm64.exe
