package sqlcheck

import (
	"context"
	"database/sql/driver"
	"errors"
	"net"
)

// ErrorKind classifies sqlcheck errors for the monitor / tray.
type ErrorKind string

const (
	// KindConn: TCP/TLS/login/database-open failure (the profile is
	// unreachable). Maps to model.ProfileStatus.Error (LevelError).
	KindConn ErrorKind = "conn"
	// KindTimeout: the operation hit its context deadline.
	KindTimeout ErrorKind = "timeout"
	// KindCanceled: the caller canceled the context (service stop).
	KindCanceled ErrorKind = "canceled"
	// KindQuery: the server rejected or failed a query.
	KindQuery ErrorKind = "query"
	// KindMapping: the mapping is missing or does not match the schema.
	KindMapping ErrorKind = "mapping"
	// KindConfig: the profile cannot be turned into a connection.
	KindConfig ErrorKind = "config"
)

// Error is the error type returned by this package.
type Error struct {
	Kind ErrorKind
	Op   string
	Err  error
	// Key is an optional localizable message key (messages.go), e.g.
	// MsgTLSEnableTLS10 for a TLS handshake failure.
	Key string
}

func (e *Error) Error() string {
	s := "sqlcheck: " + e.Op + " (" + string(e.Kind) + "): " + e.Err.Error()
	if e.Key != "" {
		s += " [" + e.Key + "]"
	}
	return s
}

func (e *Error) Unwrap() error { return e.Err }

// KindOf returns the kind of err ("" for nil or foreign errors).
func KindOf(err error) ErrorKind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return ""
}

// ErrNotConfigured is returned by ListPoints when the profile has no usable
// mapping yet (run Discovery and confirm a candidate).
var ErrNotConfigured = errors.New("mapping is not configured")

// SQL Server error numbers that mean "cannot use this profile" rather
// than "this query is wrong".
var connErrorNumbers = map[int32]bool{
	18456: true, // login failed
	18452: true, // login from untrusted domain (SSPI)
	18486: true, // account locked out
	18487: true, // password expired
	18488: true, // password must be changed
	4060:  true, // cannot open database requested by the login
	4064:  true, // cannot open user default database
	233:   true, // no process on the other end of the pipe
	10054: true, // connection reset
}

type sqlErrorNumber interface{ SQLErrorNumber() int32 }

// classify wraps err into *Error. connPhase marks errors raised while
// establishing the session (Ping / TestConnection).
func classify(op string, err error, connPhase bool) error {
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) {
		return err
	}
	if key := tlsKey(err); key != "" {
		// TLS handshake of a new session: always a connection problem.
		return &Error{Kind: KindConn, Op: op, Err: err, Key: key}
	}
	kind := KindQuery
	var ne net.Error
	var num sqlErrorNumber
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		kind = KindTimeout
	case errors.Is(err, context.Canceled):
		kind = KindCanceled
	case errors.As(err, &num) && connErrorNumbers[num.SQLErrorNumber()]:
		kind = KindConn
	case errors.As(err, &ne), errors.Is(err, driver.ErrBadConn), connPhase:
		kind = KindConn
	}
	return &Error{Kind: kind, Op: op, Err: err}
}

// isCtxErr reports whether err was caused by the context (stop discovery).
func isCtxErr(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

// ctxErr gives a bare context error (Discover stops with the caller's
// context error as is) a Kind: KindCanceled / KindTimeout, so the service
// can answer "canceled" by Kind or ConnCategory (phase 6, G1/G4). Other
// errors are returned unchanged.
func ctxErr(op string, err error) error {
	if err == nil || KindOf(err) != "" || !isCtxErr(err) {
		return err
	}
	kind := KindTimeout
	if errors.Is(err, context.Canceled) {
		kind = KindCanceled
	}
	return &Error{Kind: kind, Op: op, Err: err}
}
