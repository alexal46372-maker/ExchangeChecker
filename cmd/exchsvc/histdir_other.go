//go:build !windows

package main

// secureHistoryDir: no Windows DACL outside Windows (unit tests).
func secureHistoryDir(string) error { return nil }
