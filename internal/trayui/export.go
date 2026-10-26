package trayui

import (
	"bytes"
	"html"
	"strings"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Export of the «Состояние» rows (phase 9a, CHK-TRAY-21): CSV for Excel
// (UTF-8 with BOM, ";" separator, CRLF, RFC 4180 quoting) and a
// self-contained HTML page (escaped, no scripts, no external resources).

// csvCell quotes a cell when it holds ;, ", CR or LF; a leading =, +, -,
// @, TAB or CR gets a ' so Excel does not run it as a formula (CSV
// injection, OWASP).
func csvCell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		s = "'" + s
	}
	if strings.ContainsAny(s, ";\"\r\n") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

func headers(lang string) []string {
	h := make([]string, len(StatusColumns))
	for i, k := range StatusColumns {
		h[i] = i18n.T(k, lang)
	}
	return h
}

// ExportCSV renders rows as CSV.
func ExportCSV(rows []StatusRow, lang string) []byte {
	var b bytes.Buffer
	b.WriteString("\uFEFF")
	line := func(cells []string) {
		for i, c := range cells {
			if i > 0 {
				b.WriteByte(';')
			}
			b.WriteString(csvCell(c))
		}
		b.WriteString("\r\n")
	}
	line(headers(lang))
	for _, r := range rows {
		line(r.Cells())
	}
	return b.Bytes()
}

// htmlColors: pale state backgrounds of the theme (CHK-TRAY-51).
var htmlColors = map[Color]string{ColorGreen: PaleHTML(ColorGreen), ColorYellow: PaleHTML(ColorYellow), ColorRed: PaleHTML(ColorRed), ColorGrey: PaleHTML(ColorGrey)}

// ExportHTML renders rows as an HTML page; at is the export time (UTC+3).
func ExportHTML(rows []StatusRow, lang string, at time.Time) []byte {
	var b bytes.Buffer
	e := html.EscapeString
	title := i18n.T(i18n.KeyStTitle, lang) + " — " + i18n.T(i18n.KeyAppName, lang)
	b.WriteString("<!DOCTYPE html>\n<html lang=\"" + e(lang) + "\"><head><meta charset=\"utf-8\"><title>" + e(title) + "</title>\n")
	b.WriteString("<style>body{font-family:Segoe UI,Arial,sans-serif;font-size:13px}table{border-collapse:collapse}" +
		"td,th{border:1px solid #999;padding:3px 6px;text-align:left}th{background:#eee}</style></head><body>\n")
	b.WriteString("<h1>" + e(title) + "</h1>\n<p>" + e(i18n.T(i18n.KeyStExportedAt, lang)) + " " + e(at.In(model.DBZone).Format("02.01.2006 15:04")) + " (UTC+3)</p>\n<table>\n<tr>")
	for _, h := range headers(lang) {
		b.WriteString("<th>" + e(h) + "</th>")
	}
	b.WriteString("</tr>\n")
	for _, r := range rows {
		b.WriteString("<tr")
		if c, ok := htmlColors[r.Color]; ok {
			b.WriteString(" style=\"background:" + c + "\"")
		}
		b.WriteString(">")
		for _, c := range r.Cells() {
			b.WriteString("<td>" + e(c) + "</td>")
		}
		b.WriteString("</tr>\n")
	}
	b.WriteString("</table>\n</body></html>\n")
	return b.Bytes()
}
