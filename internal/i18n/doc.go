// Package i18n is the RU/EN string catalog of the tray UI (phase 6,
// CHK-TRAY-08). Every UI key has both a Russian and an English text; the
// default language is the Windows UI language (GetUserDefaultUILanguage,
// winapi.UserUILanguage), anything other than Russian or English falls
// back to Russian (PLAN.md §7.4, §12 p.8). Service message keys
// (monitor.*) are resolved through model.MonitorMessage.
package i18n
