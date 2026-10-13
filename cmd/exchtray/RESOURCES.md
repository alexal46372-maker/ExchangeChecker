# exchtray: ресурсы exe

`rsrc_windows_amd64.syso` — скомпилированные ресурсы трея, Go-компоновщик
подхватывает его сам при `GOOS=windows GOARCH=amd64`. С фазы 7e (T3) он
генерируется так же, как у службы и setup: `internal/winres` + `tools/mkres`
(только стандартная библиотека, без внешних тулов). Содержимое:

- `RT_MANIFEST` 1 — `exchtray.manifest`: asInvoker, Common Controls v6,
  supportedOS Windows 7 / 8 / 8.1 / 10+, без `dpiAware` (как в фазе 6a);
- `RT_GROUP_ICON` 1 + `RT_ICON` 1–3 — `exchtray.ico` (16, 32, 48 px, 32 бит
  с альфой);
- `RT_VERSION` 1 — VERSIONINFO: `FileVersion`/`ProductVersion` = версия сборки,
  `ProductName` = `Exchange Checker`, `FileDescription` = `Exchange Checker Tray`,
  `InternalName` = `exchtray`, `OriginalFilename` = `ExchangeCheckerTray.exe`;
  `CompanyName`/`LegalCopyright` не пишутся (решение 5 фазы 7).

Значки состояния в трее рисуются во время работы (`internal/trayui.IconPixels`),
`.syso` для них не нужен.

## Пересборка после правки .ico или .manifest

Из корня модуля:

```
go run ./tools/mkres          # все .syso (setup, служба, трей), версия 0.0.0-dev
go run ./tools/mkres -check   # код 1, если закоммиченный .syso устарел
```

Закоммиченный `.syso` — для `0.0.0-dev`; шаг CI «release-like build»
перегенерирует его с `-version 0.0.0-ci.N` (или версией тега) только в рабочей
копии job. Проверки: `go test ./tools/mkres ./checks -run 'CHK_RES_01|CHK_VER_01|CHK_TRAY_10'`.

Прежний `.syso` фазы 6a (akavel/rsrc) сохранён как
`internal/winres/testdata/akavel_rsrc_tray.syso` — образец независимого
генератора для теста разборщика (`TestCHK_RES_01_ParsesRsrcOutput`).
