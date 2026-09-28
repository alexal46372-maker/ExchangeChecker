# checks/CHECKS.md — манифест check ID

Реестр по PLAN.md §10. Без check ID мерж фичи запрещён (POLICY.md).

| Check ID | Статус | Что покрывает |
|----------|--------|---------------|
| `CHK-POL-01` | **implemented (phase 0)** | go 1.20 в go.mod; static scan: нет cmd.exe / powershell / wmic / sc.exe / net.exe |
| `CHK-CFG-01` | planned | Чтение/запись settings/profiles, DPAPI roundtrip |
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
