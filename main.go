// Package main starts print-bridge.
package main

import (
	"log"

	"print-bridge/internal/desktopapp"
)

func main() {
	if err := desktopapp.Run(); err != nil {
		log.Fatal(err)
	}
}
