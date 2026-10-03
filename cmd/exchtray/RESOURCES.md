# exchtray: ресурсы exe

`rsrc_windows_amd64.syso` — скомпилированные ресурсы трея (значок и манифест),
Go-компоновщик подхватывает его сам при `GOOS=windows GOARCH=amd64`.
Исходники лежат рядом:

- `exchtray.ico` — значок приложения (16, 32, 48 px, 32 бит с альфой);
- `exchtray.manifest` — asInvoker, Common Controls v6, supportedOS Windows 7+
  (PLAN.md §12).

Значки состояния в трее рисуются во время работы (`internal/trayui.IconPixels`),
`.syso` для них не нужен.

## Пересборка после правки .ico или .manifest

Генератор в go.mod не добавлен (PLAN.md §12). Утилиту ставят отдельно, вне модуля:

```
go install github.com/akavel/rsrc@v0.10.2
cd cmd/exchtray
rsrc -manifest exchtray.manifest -ico exchtray.ico -arch amd64 -o rsrc_windows_amd64.syso
```

Проверка: `go test ./checks/ -run CHK_TRAY_10` (манифест разобран, `.syso` содержит
его текст), затем `go build -ldflags="-H windowsgui" ./cmd/exchtray`.
