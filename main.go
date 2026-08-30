// Package main starts printer-bridge.
package main

import (
	"log"

	"printer-bridge/internal/desktopapp"
)

func main() {
	if err := desktopapp.Run(); err != nil {
		log.Fatal(err)
	}
}
