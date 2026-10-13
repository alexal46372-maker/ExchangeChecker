//go:build !windows

package wininstall

import (
	"errors"
	"time"
)

// ErrNotWindows: the real System lives only on Windows.
var ErrNotWindows = errors.New("wininstall: Windows only")

// NewSystem returns an error stub outside Windows (the silent CLI refuses
// to install; unit tests use a fake System).
func NewSystem() System { return stubSystem{} }

type stubSystem struct{}

func (stubSystem) MkdirAll(string) (bool, error)               { return false, ErrNotWindows }
func (stubSystem) SetDACL(string, string) error                { return ErrNotWindows }
func (stubSystem) TakeOwnership(string) (bool, error)          { return false, ErrNotWindows }
func (stubSystem) CheckFixedDrive(string) error                { return ErrNotWindows }
func (stubSystem) WriteFile(string, []byte) (bool, error)      { return false, ErrNotWindows }
func (stubSystem) BackupFile(string) (bool, error)             { return false, ErrNotWindows }
func (stubSystem) RestoreBackup(string) error                  { return ErrNotWindows }
func (stubSystem) RemoveBackup(string) error                   { return ErrNotWindows }
func (stubSystem) RemoveFile(string) (bool, error)             { return false, ErrNotWindows }
func (stubSystem) RemoveDirIfEmpty(string) (bool, error)       { return false, ErrNotWindows }
func (stubSystem) RemoveAll(string) error                      { return ErrNotWindows }
func (stubSystem) SelfDelete(string) (bool, error)             { return false, ErrNotWindows }
func (stubSystem) QueryService(string) (ServiceInfo, error)    { return ServiceInfo{}, ErrNotWindows }
func (stubSystem) CreateService(ServiceSpec) error             { return ErrNotWindows }
func (stubSystem) UpdateService(ServiceSpec, bool) error       { return ErrNotWindows }
func (stubSystem) StartService(string, time.Duration) error    { return ErrNotWindows }
func (stubSystem) StopService(string, time.Duration) error     { return ErrNotWindows }
func (stubSystem) DeleteService(string) error                  { return ErrNotWindows }
func (stubSystem) SetEventSource(string) error                 { return ErrNotWindows }
func (stubSystem) DeleteEventSource() error                    { return ErrNotWindows }
func (stubSystem) ApplyTrayRun(string, string) (string, error) { return "", ErrNotWindows }
func (stubSystem) DeleteTrayRun() error                        { return ErrNotWindows }
func (stubSystem) StopTray(string) error                       { return ErrNotWindows }
func (stubSystem) WriteUninstallKey([]RegValue) error          { return ErrNotWindows }
func (stubSystem) ReadUninstallKey() ([]RegValue, error)       { return nil, ErrNotWindows }
func (stubSystem) DeleteUninstallKey() error                   { return ErrNotWindows }
func (stubSystem) ReadInstalled() (Installed, error)           { return Installed{}, ErrNotWindows }
func (stubSystem) DeleteUserKey() error                        { return ErrNotWindows }
