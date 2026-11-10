package pcspeaker

import (
	"os"
	"strings"
)

// InpOutx64 IOCTL codes and buffer layout, taken from the Highrez / Logix4U
// sources (CHK-SVC-12). FILE_DEVICE 40000, METHOD_BUFFERED, FILE_ANY_ACCESS:
//
//	https://github.com/HardwareMonitor/OpenHardwareMonitor/blob/master/InpOut/hwinterfacedrv.h
//	lines 2–3:
//	  #define IOCTL_READ_PORT_UCHAR  -1673519100 //CTL_CODE(40000, 0x801, METHOD_BUFFERED, FILE_ANY_ACCESS)
//	  #define IOCTL_WRITE_PORT_UCHAR -1673519096 //CTL_CODE(40000, 0x802, METHOD_BUFFERED, FILE_ANY_ACCESS)
//
// As uint32: 0x9C402004 and 0x9C402008.
const (
	ioctlReadPortUchar  uint32 = 0x9C402004
	ioctlWritePortUchar uint32 = 0x9C402008
)

// writeUcharBuf is the 3-byte input of Out32 / DeviceIoControl(IOCTL_WRITE_PORT_UCHAR):
// little-endian USHORT port, then the value byte.
//
//	https://github.com/ellysh/InpOut32/blob/master/InpOut32_dll/inpout32drv.cpp
//	Out32, lines 98–111 (Buffer[3], *pBuffer = LOWORD(PortAddress), Buffer[2] = LOBYTE(data),
//	DeviceIoControl(..., IOCTL_WRITE_PORT_UCHAR, &Buffer, 3, NULL, 0, ...)).
func writeUcharBuf(port uint16, val byte) []byte {
	return []byte{byte(port), byte(port >> 8), val}
}

// readUcharIn is the 2-byte input of Inp32 / DeviceIoControl(IOCTL_READ_PORT_UCHAR).
// The result is one byte in the output buffer (Inp32 lines 139–144, 158:
// nInBufferSize=2, nOutBufferSize=1, return Buffer[0]).
//
//	https://github.com/ellysh/InpOut32/blob/master/InpOut32_dll/inpout32drv.cpp
func readUcharIn(port uint16) []byte {
	return []byte{byte(port), byte(port >> 8)}
}

// expandImagePath turns a QUERY_SERVICE_CONFIGW BinaryPathName into a
// filesystem path: strip quotes, NT prefixes \??\ and \\?\, map \SystemRoot\
// to %SystemRoot% / %WINDIR%, and expand %VAR% (Go's os.ExpandEnv does not).
// The path is taken from QueryServiceConfigW — it is never constructed here.
func expandImagePath(p string, getenv func(string) string) string {
	if getenv == nil {
		getenv = os.Getenv
	}
	p = strings.TrimSpace(p)
	if len(p) >= 2 && p[0] == '"' {
		if i := strings.IndexByte(p[1:], '"'); i >= 0 {
			p = p[1 : 1+i]
		} else {
			p = strings.Trim(p, `"`)
		}
	}
	switch {
	case strings.HasPrefix(p, `\??\`):
		p = p[4:]
	case strings.HasPrefix(p, `\\?\`):
		p = p[4:]
	}
	lower := strings.ToLower(p)
	if strings.HasPrefix(lower, `\systemroot\`) {
		root := getenv("SystemRoot")
		if root == "" {
			root = getenv("WINDIR")
		}
		root = strings.TrimRight(root, `\`)
		p = root + `\` + p[len(`\SystemRoot\`):]
	}
	return expandPercentEnv(p, getenv)
}

func expandPercentEnv(s string, getenv func(string) string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '%' {
			if j := strings.IndexByte(s[i+1:], '%'); j >= 0 {
				name := s[i+1 : i+1+j]
				if name != "" {
					if v := getenv(name); v != "" {
						b.WriteString(v)
						i = i + 2 + j
						continue
					}
				}
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
