//go:build !windows

package main

// secureHistoryDir: no Windows DACL outside Windows (unit tests).
func secureHistoryDir(string, func(format string, args ...interface{})) error { return nil }
