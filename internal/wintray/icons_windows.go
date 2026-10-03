//go:build windows

package wintray

import (
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// iconSize of the tray icons (small icon size at 96 DPI).
const iconSize = 16

// icons are generated at runtime from trayui.IconPixels (no resource
// compiler is needed for the state icons; the exe icon is in the .syso).
type icons struct{ m map[trayui.Icon]uintptr }

func (ic *icons) get(k trayui.Icon) uintptr {
	if ic.m == nil {
		ic.m = map[trayui.Icon]uintptr{}
	}
	if h, ok := ic.m[k]; ok {
		return h
	}
	h, err := winapi.CreateIconFromPixels(iconSize, iconSize, trayui.IconPixels(k, iconSize))
	if err != nil {
		return 0
	}
	ic.m[k] = h
	return h
}

func (ic *icons) free() {
	for _, h := range ic.m {
		winapi.DestroyIcon(h)
	}
	ic.m = nil
}
