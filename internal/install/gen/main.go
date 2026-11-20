//go:build ignore

// Writes deploy/proxier.service from install.Unit: go run ./internal/install/gen
package main

import (
	"os"

	"proxier/internal/install"
)

func main() {
	if err := os.WriteFile("deploy/proxier.service", []byte(install.Unit), 0o644); err != nil {
		panic(err)
	}
}
