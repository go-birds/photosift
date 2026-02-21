package buildinfo

import "runtime"

// Version can be overridden at build time using -ldflags "-X github.com/go-birds/photosift/internal/buildinfo.Version=1.0.0"
var Version = "0.1.0"

func GoVersion() string {
	return runtime.Version()
}
