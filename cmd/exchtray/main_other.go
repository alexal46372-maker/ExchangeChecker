//go:build !windows

package main

import (
	"fmt"
	"os"

	"github.com/alexal46372-maker/ExchangeChecker/internal/version"
)

func main() {
	fmt.Fprintln(os.Stderr, version.ProductName, "tray runs on Windows only")
	os.Exit(1)
}
