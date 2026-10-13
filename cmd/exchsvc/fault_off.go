//go:build !e2efault

package main

// e2eFaultHook is a no-op in every normal / release build.
func e2eFaultHook() {}
