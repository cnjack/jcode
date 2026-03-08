package main

// Version variables are declared in main.go so that `go run cmd/coding/main.go`
// works without requiring directory syntax.  Override them at build time:
//
//	go build -ldflags "-X main.Version=v1.2.3 -X main.BuildTime=$(date ...) -X main.GitCommit=$(git rev-parse --short HEAD)"
