// Package model holds pure-Go domain types for Exchange Checker
// (PLAN.md §4): Profile, Mapping, Settings, ExchangePoint, ProfileStatus
// and AlertLevel; since phase 3 also the status snapshot, alert events and
// IPC types shared by the service and the tray. It has no Windows
// dependencies and is testable on any GOOS.
//
// Check IDs: CHK-CFG-01; CHK-MON-01, CHK-MON-05, CHK-MON-06, CHK-MON-08.
package model
