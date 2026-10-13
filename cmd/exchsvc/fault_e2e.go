//go:build e2efault

package main

import "time"

// e2eFaultHook (test-only build tag e2efault, CHK-SETUP-12 E2E): the
// process starts and then hangs before connecting to the SCM dispatcher,
// so StartService ends in START_PENDING / ERROR_SERVICE_REQUEST_TIMEOUT
// while the exe stays locked. Never part of a release build.
func e2eFaultHook() {
	for {
		time.Sleep(time.Hour)
	}
}
