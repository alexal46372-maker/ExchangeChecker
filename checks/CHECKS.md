# checks/CHECKS.md — манифест check ID

Реестр по PLAN.md §10. Без check ID мерж фичи запрещён (POLICY.md).

| Check ID | Статус | Что покрывает |
|----------|--------|---------------|
| `CHK-POL-01` | **implemented (phase 0)** | go 1.20 в go.mod; static scan: нет cmd.exe / powershell / wmic / sc.exe / net.exe |
| `CHK-CI-01` | **implemented (phase 1)** | GitHub Actions: windows-latest Go 1.20.14; `go vet`/`go test`/`go build` GOOS=windows GOARCH=amd64 CGO_ENABLED=0; optional linux job. Workflow: `.github/workflows/ci.yml`. |
| `CHK-CFG-01` | **implemented (phase 1)** | model+config: Settings/Profiles/Secrets load-save, defaults, Validate, atomic write, LANGID→language, Protector (fake). Tests: `TestCHK_CFG_01_*` in `internal/model`, `internal/config`, `internal/winapi`. |
| `CHK-CFG-02` | **implemented (phase 1)** | Real DPAPI LocalMachine roundtrip + MoveFileEx atomic replace on Windows. Tests: `TestCHK_CFG_02_*` in `internal/winapi`, `internal/config` (build tag `windows`). |
| `CHK-SQL-01` | planned | TestConnection (mock + optional integration) |
| `CHK-SQL-02` | planned | Discovery: выбор таблицы/полей на фикстуре схемы |
| `CHK-SQL-03` | planned | ListPoints по сохранённому mapping |
| `CHK-SQL-04` | planned | Ручной override mapping / SQL-шаблон |
| `CHK-MON-01` | planned | Пороги warning/error на фикстурах времени |
| `CHK-IPC-01` | planned | Pipe: connect, heartbeat, ReloadConfig, StatusSnapshot |
| `CHK-IPC-02` | planned | AlertRaised → AckAlert |
| `CHK-SVC-01` | planned | Каркас службы (start/stop в тестовом харнесе) |
| `CHK-TRAY-01` | planned | Меню/команды без GUI pump; smoke GUI — отдельно |
| `CHK-SETUP-01` | planned | Инсталлятор: dry-run/registry helpers; полный UI — manual |
| `CHK-DELIV-01` | planned | Zip по DELIVERY.md без исходников |

## Как запускать CHK-POL-01

Из корня модуля:

```bash
go test ./checks/ -count=1 -v
```

Тест: `checks/pol01_test.go` (package `checks_test`).

## Как запускать CHK-CFG-01 / CHK-CFG-02

```bash
go test ./internal/model/ ./internal/config/ ./internal/winapi/ -count=1 -v
```

CHK-CFG-02 (`TestCHK_CFG_02_*`) выполняется только на Windows (CI job `windows`).

## CHK-CI-01: где смотреть результаты

Workflow `.github/workflows/ci.yml` (push в любую ветку, кроме `ci-reports`, и pull_request).
Каждый job коммитит текстовый отчёт (go version, vet, test -v, build) в orphan-ветку
`ci-reports`: `reports/<sha>/<job>-<ref>-<run_id>-<attempt>.txt` (скрипт
`.github/workflows/publish-report.sh`). Только текст, без exe/zip и без Actions artifacts.
