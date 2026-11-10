# exchtray: ресурсы exe

`rsrc_windows_amd64.syso` — скомпилированные ресурсы трея, Go-компоновщик
подхватывает его сам при `GOOS=windows GOARCH=amd64`. С фазы 7e (T3) он
генерируется так же, как у службы и setup: `internal/winres` + `tools/mkres`
(только стандартная библиотека, без внешних тулов). Содержимое:

- `RT_MANIFEST` 1 — `exchtray.manifest`: asInvoker, Common Controls v6,
  supportedOS Windows 7 / 8 / 8.1 / 10+, `dpiAware` = true (system DPI, с фазы 10 D3: окна трея масштабируются сами, шрифт Segoe UI 9);
- `RT_GROUP_ICON` 1 + `RT_ICON` 1–4 — `exchtray.ico`: 16, 32, 48 px (BMP,
  32 бит с альфой) и 256 px (PNG внутри `.ico`). С фазы 15 (CHK-TRAY-89,
  CHK-TRAY-102) — стиль «металл и хром»: тёмно-синяя плитка с градиентом в
  хромовой рамке и **зелёные** стрелки обмена с тёмной обводкой. Файл
  генерирует `tools/mkres` из `trayui.ExeICO()` (рендер
  `internal/trayui/metal.go`: float64, только стандартная библиотека,
  детерминированно); тот же значок — у окон трея
  (`internal/wintray/icons/app.ico`) и у установщика (setup берёт
  `cmd/exchtray/exchtray.ico` в свой `.syso`);
- `RT_VERSION` 1 — VERSIONINFO: `FileVersion`/`ProductVersion` = версия сборки,
  `ProductName` = `Exchange Checker`, `FileDescription` = `Exchange Checker Tray`,
  `InternalName` = `exchtray`, `OriginalFilename` = `ExchangeCheckerTray.exe`;
  `CompanyName`/`LegalCopyright` не пишутся (решение 5 фазы 7).

Значки в области уведомлений (`tray_*.ico`: тот же рисунок, цвет стрелок по
состоянию) и остальные значки окон — не в `.syso`: `tools/mkres` пишет их в
`internal/wintray/icons/` (16/20/24/32/40/48 и 256 PNG), трей встраивает их
через `embed` (CHK-TRAY-49/90).

## Пересборка после правки .ico или .manifest

Из корня модуля:

```
go run ./tools/mkres          # все .syso (setup, служба, трей), версия 0.0.0-dev
go run ./tools/mkres -check   # код 1, если закоммиченный .syso устарел
```

Закоммиченный `.syso` — для `0.0.0-dev`; шаг CI «release-like build»
перегенерирует его с `-version 0.0.0-ci.N` (или версией тега) только в рабочей
копии job. Проверки: `go test ./tools/mkres ./checks ./internal/trayui -run 'CHK_RES_01|CHK_VER_01|CHK_TRAY_10|CHK_TRAY_49|CHK_TRAY_89|CHK_TRAY_90|CHK_TRAY_10[0-2]'`.
