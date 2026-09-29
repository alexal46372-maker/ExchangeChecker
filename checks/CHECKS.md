# checks/CHECKS.md — манифест check ID

Реестр по PLAN.md §10. Без check ID мерж фичи запрещён (POLICY.md).

| Check ID | Статус | Что покрывает |
|----------|--------|---------------|
| `CHK-POL-01` | **implemented (phase 0)** | go 1.20 в go.mod; static scan: нет cmd.exe / powershell / wmic / sc.exe / net.exe |
| `CHK-CI-01` | **implemented (phase 1)** | GitHub Actions: windows-latest Go 1.20.14; `go vet`/`go test`/`go build` GOOS=windows GOARCH=amd64 CGO_ENABLED=0; optional linux job. Workflow: `.github/workflows/ci.yml`. |
| `CHK-CFG-01` | **implemented (phase 1)** | model+config: Settings/Profiles/Secrets load-save, defaults, Validate, atomic write, LANGID→language, Protector (fake). Tests: `TestCHK_CFG_01_*` in `internal/model`, `internal/config`, `internal/winapi`. |
| `CHK-CFG-02` | **implemented (phase 1)** | Real DPAPI LocalMachine roundtrip + MoveFileEx atomic replace on Windows. Tests: `TestCHK_CFG_02_*` in `internal/winapi`, `internal/config` (build tag `windows`). |
| `CHK-SQL-01` | **implemented (phase 2)** | TestConnection: драйвер `github.com/microsoft/go-mssqldb v1.8.2` (go 1.18, совместим с Go 1.20), конфиг без пароля в DSN, host\instance/порт, Encrypt disable/optional/on/strict + TLSMinVersion + TrustServerCert, SQL/Windows Auth (winsspi), таймауты, классификация ошибок; CI-шаг: нет `os/exec` в `go list -deps` (windows/amd64). Tests: `TestCHK_SQL_01_DriverConfig`, `_DriverConfigAuth`, `_ErrorClassify`, `_ReaderTest` (`internal/sqlcheck`), `TestCHK_SQL_01_ProfileConnOptions` (`internal/model`), `TestCHK_SQL_01_IntegrationConnect` (реальный SQL Server). |
| `CHK-SQL-02` | **implemented (phase 2)** | Discovery без зависимости от `_Node*` (`_Node` = +10): same_table / linked по binary(16) / generic / counters-fallback, YearOffset 0/2000, скоринг и ранжирование, бюджеты и лимиты, NOLOCK, экранирование; **DiscoverByValue** (имя точки + примерное время, LIKE с литералами `%`/`_`, даты из той же или связанной строки). Фикстуры: `_Node123`+`_InfoRg456` (сдвиг 2000 и 0), `_Reference77`+`_InfoRg88` без `_Node`, `_Reference55` с датами, не-1С схема. Tests: `TestCHK_SQL_02_Discover1CNodeOffset2000`, `_Discover1CNodeNoOffset`, `_DiscoverCustomWithoutNode`, `_DiscoverDatesInDirectory`, `_DiscoverGenericSchema`, `_ScoringNodeBonusAndFreshness`, `_DiscoverDirectoryRowLimit`, `_DiscoverWithoutRowCounts`, `_DiscoverSkipsFailingTable`, `_DiscoverTimeBudget`, `_DetectYearOffset`, `_SendRecvSelection`, `_CandidateJSONAndMapping`, `_QuoteIdent`, `_LikeContains`, `_SQLBuilders`, `_DiscoverByValueLinked`, `_DiscoverByValueScoring`, `_DiscoverByValueSameRow`, `_DiscoverByValueLimits` (`internal/sqlcheck`), `TestCHK_SQL_02_CandidateMappingAndValueQuery` (`internal/model`), `TestCHK_SQL_02_IntegrationDiscover`, `_IntegrationDiscoverByValue`. |
| `CHK-SQL-03` | **implemented (phase 2)** | ListPoints по сохранённому mapping (single / linked INNER/LEFT JOIN, YearOffset, пустые даты 1С → NULL → Error `no_timestamps`, UTC+3), Status (ошибка соединения → `ProfileStatus.Error`); **режим `counters`**: чтение `_SentNo`/`_ReceivedNo` из `_Node*` (NOLOCK), чистая `model.ApplyCounters` (первое наблюдение = baseline, без изменений, send / recv / оба изменились, уменьшение/сброс = изменение, точка добавлена/удалена, now в UTC+3), JSON-состояние, `CounterStateStore` + `MemCounterStore`, кандидат counters с низким score. Tests: `TestCHK_SQL_03_ConvertDBTime`, `_ListPointsLinked`, `_ListPointsSingleAndSettings`, `_ListPointsErrors`, `_StatusConnError`, `_CountersReader`, `_DiscoverOffersCountersFallback` (`internal/sqlcheck`), `TestCHK_SQL_03_ApplyCountersCases`, `_ApplyCountersZone`, `_CounterStateJSONAndStore` (`internal/model`), `TestCHK_SQL_03_IntegrationListPoints`, `_IntegrationCounters`. |
| `CHK-SQL-04` | **implemented (phase 2)** | Ручной override mapping / SQL-шаблон: `ValidateManualSQL` (один SELECT/WITH, комментарии/литералы, `;` только в конце), выполнение в откатываемой READ UNCOMMITTED транзакции, ровно 3 колонки; `CheckMapping`/`ValidateMapping` (таблица/колонка не найдена, неоднозначна, тип, тип связи), `PreviewMapping`, `ReaderFor`; валидация расширенного Mapping; обратная совместимость profiles.json. Tests: `TestCHK_SQL_04_ValidateManualSQL`, `_QueryReader`, `_CheckMappingOverride`, `_PreviewMapping` (`internal/sqlcheck`), `TestCHK_SQL_04_MappingExtendedValidation` (`internal/model`), `TestCHK_SQL_04_ProfilesPhase2Fields` (`internal/config`), `TestCHK_SQL_04_IntegrationManualMapping`. |
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

Workflow `.github/workflows/ci.yml` запускается при push в любую ветку и при `pull_request`. Результаты обоих job читаются через GitHub Actions API; Actions artifacts и отдельная ветка отчётов не используются.

## Как запускать CHK-SQL-01 … CHK-SQL-04

Unit-тесты (фейковый Store с фикстурами схем, без сервера):

```bash
go test ./internal/sqlcheck/ ./internal/model/ ./internal/config/ -run 'CHK_SQL' -count=1 -v
```

Для интеграционных тестов (`TestCHK_SQL_0x_Integration*`) нужен SQL Server; без `EC_SQL_TEST_HOST` они пропускаются (так в windows-job). В CI их запускает linux-job с контейнером `mcr.microsoft.com/mssql/server:2019-latest`. Тесты сами создают базы `ec_1c_node_off`, `ec_1c_node_plain`, `ec_1c_custom`, `ec_1c_dirdates`, `ec_generic`, пересоздавая их при каждом запуске:

```bash
EC_SQL_TEST_HOST=127.0.0.1 EC_SQL_TEST_PORT=1433 EC_SQL_TEST_USER=sa EC_SQL_TEST_PASSWORD='...' \
  go test ./internal/sqlcheck/ -run 'CHK_SQL_0[1-4]_Integration' -count=1 -v
```

Проверка графа зависимостей (CHK-SQL-01, шаг CI windows-job):

```bash
GOOS=windows GOARCH=amd64 go list -deps ./cmd/... ./internal/... | grep -x os/exec   # пусто = OK
```
