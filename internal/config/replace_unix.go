//go:build !windows

package config

import "os"

func replaceFile(src string, dst string) error {
	return os.Rename(src, dst)
}
