//go:build !windows

package main

import "errors"

func runAsService(options) (bool, error) { return false, nil }

func manageService([]string) error {
	return errors.New("service management is only available on Windows; use systemd (Linux) or launchd (macOS)")
}
