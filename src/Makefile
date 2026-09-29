all: m3s

m3s:
	@echo "Building for local os/arch..."
	go build -o m3s
	chmod u+x m3s

devbuild: clean m3s

updateDep:
	go mod tidy

clean:
	rm -f m3s
