//go:build windows

package winapi

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

// Client authorization of the service's named pipe (phase 6, CHK-IPC-06):
// ImpersonateNamedPipeClient -> OpenThreadToken -> CheckTokenMembership
// (BUILTIN\Administrators) -> RevertToSelf. A UAC-filtered admin token has
// Administrators as a deny-only group, so CheckTokenMembership reports
// false: only an elevated administrator (or SYSTEM, or UAC off) passes.
var (
	procImpersonateNamedPipeClient = modadvapi32.NewProc("ImpersonateNamedPipeClient")
	procRevertToSelf               = modadvapi32.NewProc("RevertToSelf")
	procOpenThreadToken            = modadvapi32.NewProc("OpenThreadToken")
	procCheckTokenMembership       = modadvapi32.NewProc("CheckTokenMembership")
	procImpersonateLoggedOnUser    = modadvapi32.NewProc("ImpersonateLoggedOnUser")
	procCreateRestrictedToken      = modadvapi32.NewProc("CreateRestrictedToken")
	procGetNamedSecurityInfoW      = modadvapi32.NewProc("GetNamedSecurityInfoW")
	procConvertSDToStringSDW       = modadvapi32.NewProc("ConvertSecurityDescriptorToStringSecurityDescriptorW")
	procGetCurrentThread           = modkernel32.NewProc("GetCurrentThread")
)

// SIDAdministrators is BUILTIN\Administrators.
const SIDAdministrators = "S-1-5-32-544"

// ErrorCannotImpersonate is ERROR_CANNOT_IMPERSONATE (1368): the pipe
// server tried to impersonate before reading data from the client.
const ErrorCannotImpersonate syscall.Errno = 1368

// PipeClient is the identity of a pipe client.
type PipeClient struct {
	// Admin: the client token is a (non deny-only) member of
	// BUILTIN\Administrators, i.e. an elevated administrator.
	Admin bool
	// Account is DOMAIN\user of the client ("" if unknown).
	Account string
}

// ImpersonatePipeClient checks the client of the server end h of a named
// pipe. It runs on a dedicated OS thread (LockOSThread without Unlock: the
// thread is discarded when the goroutine ends, so an impersonation can
// never leak to another goroutine even if RevertToSelf failed). Any error
// (anonymous client, impersonation before the first read, ...) means "not
// authorized"; the caller must treat err != nil as Admin == false.
func ImpersonatePipeClient(h syscall.Handle) (PipeClient, error) {
	type result struct {
		c   PipeClient
		err error
	}
	ch := make(chan result, 1)
	go func() {
		runtime.LockOSThread() // never unlocked: the thread exits with the goroutine
		c, err := impersonateOnThread(h)
		ch <- result{c, err}
	}()
	r := <-ch
	return r.c, r.err
}

func impersonateOnThread(h syscall.Handle) (PipeClient, error) {
	if r, _, e := procImpersonateNamedPipeClient.Call(uintptr(h)); r == 0 {
		return PipeClient{}, fmt.Errorf("winapi: ImpersonateNamedPipeClient: %w", e)
	}
	var (
		c        PipeClient
		err      error
		sid      *syscall.SID
		tokenBuf *syscall.Tokenuser
	)
	func() {
		defer func() {
			if r, _, e := procRevertToSelf.Call(); r == 0 {
				// the thread stays locked and dies with the goroutine
				err = fmt.Errorf("winapi: RevertToSelf: %w", e)
				c = PipeClient{}
			}
		}()
		tok, terr := openThreadToken()
		if terr != nil {
			err = terr
			return
		}
		defer tok.Close()
		admin, merr := TokenIsMember(tok, SIDAdministrators)
		if merr != nil {
			err = merr
			return
		}
		c.Admin = admin
		if tu, uerr := tok.GetTokenUser(); uerr == nil {
			tokenBuf = tu
			sid = tu.User.Sid
		}
	}()
	if err != nil {
		return PipeClient{}, err
	}
	if sid != nil {
		// looked up as the service, after RevertToSelf
		if user, dom, _, lerr := sid.LookupAccount(""); lerr == nil {
			c.Account = dom + `\` + user
		} else if s, serr := sid.String(); serr == nil {
			c.Account = s
		}
	}
	runtime.KeepAlive(tokenBuf)
	return c, nil
}

func openThreadToken() (syscall.Token, error) {
	th, _, _ := procGetCurrentThread.Call()
	var tok syscall.Token
	// OpenAsSelf = TRUE: access check against the service's own token
	r, _, e := procOpenThreadToken.Call(th, syscall.TOKEN_QUERY, 1, uintptr(unsafe.Pointer(&tok)))
	if r == 0 {
		return 0, fmt.Errorf("winapi: OpenThreadToken: %w", e)
	}
	return tok, nil
}

// TokenIsMember reports CheckTokenMembership(tok, sid) (tok 0 = the
// calling thread's impersonation token, or a copy of the primary token).
func TokenIsMember(tok syscall.Token, sidString string) (bool, error) {
	sid, err := syscall.StringToSid(sidString)
	if err != nil {
		return false, err
	}
	var member int32
	r, _, e := procCheckTokenMembership.Call(uintptr(tok), uintptr(unsafe.Pointer(sid)), uintptr(unsafe.Pointer(&member)))
	if r == 0 {
		return false, fmt.Errorf("winapi: CheckTokenMembership: %w", e)
	}
	return member != 0, nil
}

// ProcessIsAdmin reports whether the current process token is an elevated
// administrator (tests: skip admin-positive checks on a non-elevated
// runner).
func ProcessIsAdmin() (bool, error) { return TokenIsMember(0, SIDAdministrators) }

// RevertToSelf ends the impersonation of the calling thread.
func RevertToSelf() error {
	if r, _, e := procRevertToSelf.Call(); r == 0 {
		return e
	}
	return nil
}

// ImpersonateLoggedOnUser impersonates tok on the calling thread (tests:
// pipe client with a restricted token). Lock the OS thread first.
func ImpersonateLoggedOnUser(tok syscall.Token) error {
	if r, _, e := procImpersonateLoggedOnUser.Call(uintptr(tok)); r == 0 {
		return e
	}
	return nil
}

type sidAndAttributes struct {
	Sid        *syscall.SID
	Attributes uint32
}

// DenyOnlyToken returns a restricted copy of the process token in which the
// groups sids are deny-only (CreateRestrictedToken SidsToDisable) — the
// same shape as a UAC-filtered admin token (tests).
func DenyOnlyToken(sids ...string) (syscall.Token, error) {
	var proc syscall.Token
	p, _ := syscall.GetCurrentProcess()
	if err := syscall.OpenProcessToken(p, syscall.TOKEN_DUPLICATE|syscall.TOKEN_QUERY|syscall.TOKEN_ASSIGN_PRIMARY|syscall.TOKEN_IMPERSONATE, &proc); err != nil {
		return 0, fmt.Errorf("winapi: OpenProcessToken: %w", err)
	}
	defer proc.Close()
	if len(sids) == 0 {
		return 0, errors.New("winapi: DenyOnlyToken: no sids")
	}
	dis := make([]sidAndAttributes, len(sids))
	for i, s := range sids {
		sid, err := syscall.StringToSid(s)
		if err != nil {
			return 0, err
		}
		dis[i].Sid = sid
	}
	var out syscall.Token
	r, _, e := procCreateRestrictedToken.Call(uintptr(proc), 0, uintptr(len(dis)), uintptr(unsafe.Pointer(&dis[0])),
		0, 0, 0, 0, uintptr(unsafe.Pointer(&out)))
	runtime.KeepAlive(dis)
	if r == 0 {
		return 0, fmt.Errorf("winapi: CreateRestrictedToken: %w", e)
	}
	return out, nil
}

const (
	seFileObject            = 1
	daclSecurityInformation = 0x4
	fileAttributeNormal     = 0x80
	createNew               = 1
)

// FileDACL returns the DACL of path as SDDL ("D:P(A;;FA;;;SY)...")
// (GetNamedSecurityInfoW + ConvertSecurityDescriptorToStringSecurity-
// DescriptorW).
func FileDACL(path string) (string, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	var dacl, sd uintptr
	r, _, _ := procGetNamedSecurityInfoW.Call(uintptr(unsafe.Pointer(p)), seFileObject, daclSecurityInformation,
		0, 0, uintptr(unsafe.Pointer(&dacl)), 0, uintptr(unsafe.Pointer(&sd)))
	if r != 0 {
		return "", &os.PathError{Op: "GetNamedSecurityInfo", Path: path, Err: syscall.Errno(r)}
	}
	defer procLocalFree.Call(sd)
	var str *uint16
	r, _, e := procConvertSDToStringSDW.Call(sd, sddlRevision1, daclSecurityInformation, uintptr(unsafe.Pointer(&str)), 0)
	if r == 0 {
		return "", fmt.Errorf("winapi: ConvertSecurityDescriptorToStringSecurityDescriptorW: %w", e)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(str)))
	return utf16PtrToString(str), nil
}

func utf16PtrToString(p *uint16) string {
	if p == nil {
		return ""
	}
	var s []uint16
	for ptr := unsafe.Pointer(p); ; ptr = unsafe.Pointer(uintptr(ptr) + 2) {
		c := *(*uint16)(ptr)
		if c == 0 {
			break
		}
		s = append(s, c)
	}
	return syscall.UTF16ToString(s)
}

// CreateFileSDDL creates a new file (CREATE_NEW) with the security
// descriptor sddl applied at creation, so the data is never readable under
// an inherited DACL (secrets.dpapi, CHK-CFG-03).
func CreateFileSDDL(path, sddl string) (*os.File, error) {
	sa, free, err := SecurityAttributesFromSDDL(sddl)
	if err != nil {
		return nil, err
	}
	defer free()
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, sa, createNew, fileAttributeNormal, 0)
	if err != nil {
		return nil, &os.PathError{Op: "CreateFile", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}
