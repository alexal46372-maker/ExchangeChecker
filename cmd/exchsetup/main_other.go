//go:build !windows

package main

import (
	"fmt"
	"os"

	"github.com/alexal46372-maker/ExchangeChecker/internal/version"
)

func main() {
	fmt.Fprintf(os.Stderr, "%s %s: Windows only\n", version.ProductName, version.Version)
	os.Exit(1)
}
