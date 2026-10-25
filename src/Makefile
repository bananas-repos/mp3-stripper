all: m3s

m3s:
	@echo "Building for local os/arch..."
	go build -o m3s

crosscompile:
	@echo "Building for windows..."
	GOOS=windows GOARCH=amd64 go build -o m3s-windows-amd64.exe
	@echo "Building for linux..."
	GOOS=linux GOARCH=amd64 go build -o m3s-linux-amd64
	@echo "Building for darwin ..."
	GOOS=darwin GOARCH=arm64 go build -o m3s-darwin-arm64

devbuild: clean m3s
	chmod u+x m3s

updateDep:
	go mod tidy

clean:
	rm -f m3s
