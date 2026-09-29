//go:build !windows

package winsvc

import "context"

// Run always returns ErrNotService outside Windows (console mode).
func Run(name string, fn func(ctx context.Context) error) error { return ErrNotService }
