# PLAN.md — детальный план Exchange Checker

Статус: решения §12 п.2–9 **приняты** (см. §12). SQL Discovery — §5.3. **Фаза 0 и Фаза 1 выполнены.** Язык UI по умолчанию при первом запуске = язык ОС Windows (если не RU/EN — fallback RU).

Связанные документы: [`POLICY.md`](POLICY.md), позже [`DELIVERY.md`](DELIVERY.md).

---

## 1. Цель продукта

На одной Windows-машине:

1. **Служба** постоянно опрашивает указанные базы Microsoft SQL Server, читает по точкам обмена 1С 8.1 даты/время последней **отправки** и **приёма** сообщений, сравнивает с порогами warning/error.
2. **Трей** только UI: настройки, тест SQL, просмотр статусов, модальные алерты + звук PC speaker.
3. **Инсталлятор** ставит оба `.exe`, ярлыки, запись в «Установка и удаление программ», опциональный автозапуск службы.

Связь служба ↔ трей — **постоянное** соединение (не «раз в N секунд скинуть файл»).

---

## 2. Компоненты и артефакты сборки

| Компонент | Путь исходников | Имя exe (рабочее) | Роль |
|-----------|-----------------|-------------------|------|
| Служба | `cmd/exchsvc` | `ExchangeCheckerSvc.exe` | Windows Service, опрос SQL, оценка порогов, IPC-сервер, команда алертов трею |
| Трей | `cmd/exchtray` | `ExchangeCheckerTray.exe` | NotifyIcon, меню, окна настроек/«О программе», статус, алерт-UI + Beep |
| Инсталлятор | `cmd/exchsetup` | `ExchangeCheckerSetup.exe` | Wizard: папка, ярлыки, Uninstall, галочка автозапуска службы |

Поставка заказчику (после явной просьбы собрать): zip по `DELIVERY.md` с `ExchangeCheckerSetup.exe` (внутри или рядом — служба и трей по схеме инсталлятора).

Toolchain (жёстко): **Go 1.20.14**, `GOOS=windows` `GOARCH=amd64` `CGO_ENABLED=0`, цель **NT 6.1+**.

---

## 3. Дерево репозитория (целевое)

```
/
  POLICY.md
  PLAN.md
  DELIVERY.md
  README.md
  go.mod                    # go 1.20
  go.sum
  cmd/
    exchsvc/                # служба
    exchtray/               # трей
    exchsetup/              # инсталлятор
  internal/
    config/                 # чтение/запись настроек, DPAPI для паролей
    model/                  # Profile, ExchangePoint, Status, AlertLevel
    sqlcheck/               # MSSQL-запросы времён обмена + TestConnection
    monitor/                # цикл опроса, сравнение с порогами
    ipc/                    # именованный канал (named pipe) служба↔трей
    alert/                  # правила warning/error, тексты, коды звука
    winsvc/                 # обёртка Windows Service (x/sys/windows/svc)
    wintray/                # NotifyIcon, меню, окна (Win32)
    wininstall/             # wizard, Uninstall registry, shortcuts, SCM
    hcwinnet/               # сеть только через этот пакет (POLICY)
    winapi/                 # тонкие обёртки syscall / x/sys/windows
  testdata/                 # фикстуры SQL/ответов IPC для автотестов
  checks/                   # манифест check ID ↔ тесты/фичи
```

Принцип: общая логика в `internal/*`, UI и SCM — только в `cmd` + `internal/win*`. Без вызова `cmd.exe` / PowerShell / `sc` / `net` / `wmic`.

---

## 4. Модель данных и настройки

### 4.1. Файлы конфигурации

Каталог по умолчанию (после установки):

`%ProgramData%\ExchangeChecker\`

| Файл | Содержание | Кто пишет |
|------|------------|-----------|
| `settings.json` | интервал опроса, пороги warning/error (минуты), параметры звука, общие флаги | трей (настройки) |
| `profiles.json` | список профилей SQL (без паролей в открытом виде) | трей |
| `secrets.dpapi` | пароли профилей, защищённые `CryptProtectData` с **`CRYPTPROTECT_LOCAL_MACHINE`** (+ `UI_FORBIDDEN`); см. §12 п.9 | трей |
| `state.json` | последний снимок статусов (опциональный кэш для трея при старте до ответа службы) | служба |

Служба и трей читают одни и те же файлы; **источник истины по статусу обмена — служба** (живой снимок по IPC). Трей при сохранении настроек уведомляет службу по IPC: `ReloadConfig`.

### 4.2. Профиль (логически)

```text
Profile {
  ID            string   // uuid
  DisplayName   string
  SQLHost       string   // IP или hostname
  SQLPort       int      // по умолчанию 1433
  Database      string
  AuthMode      string   // "sql" | "windows" — оба режима обязательны в v1
  Username      string   // для sql auth
  // Password — только в secrets.dpapi
  Enabled       bool
  // Опционально override порогов/интервала на профиль — фаза 2
}
```

### 4.3. Глобальные настройки мониторинга

```text
MonitorSettings {
  PollIntervalSec     int   // как часто служба чекает все базы
  WarningAfterMin     int   // простой обмена ≥ N минут → warning
  ErrorAfterMin       int   // простой ≥ M минут → error (M > Warning)
  SoundWarningHz      int   // частота Beep для warning
  SoundWarningMs      int   // длительность одного Beep
  SoundErrorHz        int
  SoundErrorMs        int
  SoundGapMs          int   // пауза в бесконечном цикле писка
}
```

«Простой» считаем по **одной «свежей» дате**: `FreshAt = max(LastSend, LastRecv)` (NULL игнорируем). Лаг = `now(UTC+3) − FreshAt`. Алерт по точке, если лаг ≥ порога warning/error. Отдельные пороги на send/recv не используются.

### 4.4. Точка обмена (результат опроса)

```text
ExchangePointStatus {
  ProfileID     string
  PointName     string   // имя узла / точки из БД
  LastSendUTC   time     // nullable
  LastRecvUTC   time     // nullable
  SendLagMin    int
  RecvLagMin    int
  FreshAtUTC3   time     // max(LastSend, LastRecv) в шкале UTC+3
  FreshLagMin    int      // лаг от FreshAt — для алертов
  Level         OK | Warning | Error | Unknown | ConnError
  Message       string
  CheckedAt     time
}
```


### 4.5. Часовой пояс (принято)

- Даты/время **в MSSQL** интерпретируются как **UTC+3** точно (фиксированный сдвиг +3 часа от UTC, без отдельной DST-логики в продукте).
- «Сейчас» для лага — та же шкала **UTC+3**.
- В UI показываем UTC+3; в IPC/state — одна согласованная схема хранения (не смешивать naive и instant).

---

## 5. MSSQL: что читаем

### 5.1. Задача

В каждой указанной базе 1С 8.1 (файловый вариант не целевой — только SQL) найти по **точкам обмена** две метки:

- дата/время последней **отправки** сообщений;
- дата/время последнего **приёма** сообщений.

### 5.2. Стратегия доступа

1. Драйвер (выбран в фазе 2, `CHK-SQL-01`): **`github.com/microsoft/go-mssqldb v1.8.2`** — чистый Go TDS, без CGO; последняя версия с `go 1.18` в go.mod (v1.9.0+ требует go ≥ 1.23 — несовместимо с Go 1.20). Транзитивно компилируются: `golang-sql/civil v0.0.0-20220223132316-b832511892a9`, `golang-sql/sqlexp v0.1.0`, `google/uuid v1.6.0`, `golang.org/x/crypto v0.24.0` (md4 для NTLM), `golang.org/x/text v0.16.0` — все объявляют go ≤ 1.18. Azure/Kerberos-модули драйвера не импортируются. `os/exec` в графе сборки нет (windows/amd64 и linux/amd64; CI-шаг `go list -deps … | grep -x os/exec`). Windows Auth: `integratedauth/winsspi` (secur32.dll через syscall).
   - TLS: `Encrypt` ""/optional = умолчание драйвера (TLS только для логина, сертификат не проверяется), `disable` — без TLS, `on` (+`TrustServerCert`), `strict` (TDS 8, SQL 2022+). Go 1.20 по умолчанию требует TLS ≥ 1.2: для SQL 2005/2008 без TLS 1.2 — `TLSMinVersion "1.0"` или `Encrypt "disable"`.
   - Таймауты: `dial timeout` = ConnectTimeoutSec; остальное — через context (QueryTimeoutSec на запрос); `connection timeout` драйвера (per-IO deadline) не используется.
2. Подключение: host/port/db; **SQL Authentication и Windows Authentication — оба обязательны** в v1 (выбор AuthMode в профиле).
3. Запрос времён — **сначала авто-Discovery + сохранённый mapping** (§5.3); ручной SQL-шаблон в профиле — запасной режим для нестандартных баз.

### 5.3. Автопоиск таблицы и полей (основной способ)

Заказчик **не обязан** знать имена таблиц 1С. Софт сам ищет кандидатов в указанной базе и предлагает выбор в GUI («Найти таблицы» / при первом «Тест»).

**Алгоритм Discovery (служба, `internal/sqlcheck/discover`):** (исходный набросок)

1. `TestConnection` — логин в MSSQL успешен.
2. Читаем каталог и отбираем кандидатов эвристиками; для кандидата подбираем `PointName`, `LastSend`/`LastRecv`; считаем score; топ-N отдаём трею; пользователь подтверждает; опрос — только по сохранённому mapping; ручной SQL — запасной путь.

**Реализация (фаза 2, `internal/sqlcheck`).** Discovery **не зависит от `_Node*`**: универсальный путь работает для любой конфигурации 1С (планы обмена, самописные справочники+регистры, даты прямо в справочнике) и для не-1С схем. Все запросы — **только чтение**: `SET TRANSACTION ISOLATION LEVEL READ UNCOMMITTED` в каталожных батчах + `WITH (NOLOCK)` на каждой пользовательской таблице, `TOP (@p1)` везде, имена — только через `QuoteIdent` (`[...]`, `]`→`]]`, длина ≤128), значения — только параметрами.

**Алгоритм Discovery (`Discover`):**

1. Каталог: `sys.columns`/`sys.tables`/`sys.schemas`/`sys.types` (одним запросом). Число строк: `sys.partitions` (index_id 0/1), fallback — `sysindexes` (SQL 2005 без прав на DMV); нет ни того ни другого — работаем без row counts (лимиты по TOP).
2. **Справочники точек** — таблицы `_Reference*`, `_Node*`, `_InfoRg*`, `_Document*` и не-1С таблицы с ≤ `MaxDirRows` (5000) строк, у которых есть строковая колонка имени (`_Description`, иначе `_Code`, иначе строковая колонка ≤ 4000 символов / маркеры name/node/point/узел).
3. **Источники кандидатов:**
   - *same_table* — две (или одна) datetime-серии в самом справочнике (`_Reference55._Fld56/_Fld57`, база 35);
   - *linked* — таблица времён (`_InfoRg*`/`_Document*`/любая) с колонкой `binary(16)`, чьи значения совпадают с `_IDRRef` справочника (выборка до 500 ссылок → доля совпадений и покрытие точек; база 35 + ratio·10 + coverage·5, `_InfoRg` +2);
   - *generic* — не-1С таблица с именем точки и datetime-колонками (маркеры exchange/node/sync/send/recv…, база 25);
   - *counters* — **fallback** для конфигураций без хранимых времён: при наличии `_Node*` с `_SentNo`/`_ReceivedNo` кандидат с фиксированным **низким** score 5, hint `counters_fallback`, Reason объясняет, что это запасной режим.
   - Бонус `_Node*` — только **+10** (hint `node_table`), не условие.
4. **Серии времени** (`evaluateSeries`): MAX/COUNT по каждой datetime-колонке (до 16 колонок), поиск `YearOffset` (MAX год > now+1000 → 2000), свежесть MAX после сдвига: ≤1ч +30, ≤1д +25, ≤7д +18, ≤30д +10, ≤1г +4, иначе +1; «будущее» −10; две серии +10; send/recv по именам (+5, частично +2), иначе две самые свежие по порядку колонок (hint `send_recv_by_order` — подтвердить в GUI).
5. **Ранжирование** (`rank`, топ-30): реальный ListPoints по кандидату: +15·(доля точек, обновлённых в пределах 7 дней от freshest), +5·(доля точек с обеими датами), +5 за 2..500 точек (hint `all_points_fresh`).
6. **Бюджеты:** общий `Timeout` 90 с, `QueryTimeout` 15 с на запрос, `MaxDirTables` 300, `MaxTimesTables` 400, `StatsRowLimit` 200 000, `MaxLinkedRows` 500 000, `MaxLinkTables` 200. Ошибка/таймаут одной таблицы — таблица пропускается; исчерпан общий бюджет — частичный результат + hint `truncated`; отмена родительского ctx — ошибка.

**`DiscoverByValue(ValueQuery{PointName, ApproxTime?, ToleranceMin})`** — пользователь вводит имя знакомой точки (и, опционально, примерное время последнего обмена):

1. `LIKE N'%…%'` (экранирование `[`, `%`, `_` → литералы) по строковым колонкам (≤ 24 на таблицу, ≤ 1024 символов) таблиц с ≤ `MaxValueRows` (100 000) строк, ≤ 300 таблиц, `MatchRows` 20 на таблицу.
2. Даты берутся из **той же строки** или из **связанных строк** других таблиц по `binary(16)`-ссылке (`WHERE ref = @p2`, ≤ `MaxMatches` 20 совпадений).
3. Score: база 50; точное совпадение +15 (hint `exact_match`) / подстрока +5; близость ближайшей даты к ApproxTime ≤ 1 ч +30, ≤ tolerance +20 (обе — hint `near_approx_time`), ≤ 7 дн +8; без ApproxTime — свежесть; `_Node` +10, `_InfoRg` +2. Результат — те же `DiscoveryCandidate` + `Matched` (найденная строка).

**Типы (IPC-ready JSON, пакет `internal/model`, без драйвера):**

```go
DiscoveryCandidate { Mode single|linked|counters, Schema, Table, PointCol, PointIDCol, PointKeyCol,
    SendCol, RecvCol, TimesSchema, TimesTable, LinkCol, YearOffset, FreshestAt, Points,
    Matched *SampleRow, SampleRows []SampleRow, Score, Reason, Hints []string }  // .Mapping()
ValueQuery { PointName, ApproxTime *time.Time, ToleranceMin }                    // default 1440
```

6. Пользователь **подтверждает** кандидата (или правит поля вручную). В профиле сохраняется (все новые поля `omitempty` — профили фазы 1 читаются без изменений):

```text
Profile.Mapping {
  Mode               "" | "auto" | "manual" | "counters"
  Schema             string   // "" = dbo
  Table              string   // справочник точек (или таблица с датами)
  PointNameColumn    string
  PointIDColumn      string
  PointKeyColumn     string   // _IDRRef: ключ для linked / counters
  TimesSchema, TimesTable, TimesLinkColumn string   // linked: TimesTable.TimesLinkColumn = Table.PointKeyColumn
  KeepUnlinkedPoints bool     // LEFT JOIN вместо INNER (точки без строки времён → no_timestamps)
  LastSendColumn, LastRecvColumn string
  YearOffset         0 | 2000 // сдвиг лет 1С
  SentNoColumn, ReceivedNoColumn string  // counters: по умолчанию _SentNo/_ReceivedNo
  ManualSQL          string
}
Profile: + Encrypt ""|optional|disable|on|strict, TrustServerCert, TLSMinVersion ""|1.0|1.1|1.2,
           ConnectTimeoutSec (15), QueryTimeoutSec (30)
```

7. Рабочий опрос (`ListPoints`) идёт **только по сохранённому mapping**, без повторного угадывания (тоже `NOLOCK`). Даты: naive → минус `YearOffset` → `FromDBNaive` (UTC+3); год < 1900 после сдвига (пустая дата 1С `2001-01-01`/`1753-01-01`) → NULL; обе NULL → Error `no_timestamps` (решение фазы 1). Linked: `INNER JOIN` + `MAX()` по точке (точки без строки времён пропускаются; `keepUnlinkedPoints` → `LEFT JOIN`).
8. **Ручной mapping / SQL-шаблон — полноценный режим (CHK-SQL-04):** `ValidateMapping` (таблицы/колонки существуют, типы: datetime для времён, строка/число для имени, `binary` одинаковой длины для связи, числа для счётчиков; неоднозначное имя таблицы без схемы → ошибка), `PreviewMapping` (первые N строк), понятные ошибки `ErrTableNotFound`/`ErrTableAmbiguous`/`ErrColumnNotFound`/`ErrColumnType`/`ErrLinkTypeMismatch`/`ErrNoTimestampColumn` с `schema.table.column`. `ManualSQL`: ровно один `SELECT`/`WITH` (комментарии и литералы учитываются, `;` только в конце), выполняется в транзакции `READ UNCOMMITTED`, которая **всегда откатывается**, ровно 3 колонки (имя, send, recv); `YearOffset` применяется к результату. Если заданы и таблица, и ManualSQL — используется ManualSQL (`QueryReader`).
9. **Режим `Mode="counters"`** (решение архитектора, через Вику) — для конфигураций, где времена обмена не хранятся вообще:
   - `ListPoints` читает `_SentNo`/`_ReceivedNo` (+ `_Code`/`_Description`/`_IDRRef`) из одной таблицы `_Node*` (`NOLOCK`, имена экранированы, `TOP (@p1)`);
   - чистая функция `model.ApplyCounters(prev CounterState, observed []CounterObservation, now) (next CounterState, []CounterTimes)`: новая точка → время = now, флаги baseline; счётчик не изменился → время и флаг сохраняются; **любое** изменение (в т.ч. **уменьшение/сброс** — 1С пересоздал узел или сбросил нумерацию) → время = now, baseline снимается; исчезнувшие точки удаляются из состояния; дубликаты ключа — берётся первый; now приводится к UTC+3;
   - ключ точки: hex `_IDRRef`, иначе `code:`/`name:`;
   - `ExchangePoint` получает `source:"counters"`, `sendBaseline`/`recvBaseline` (`omitempty`, обратно совместимо) — UI пишет «не раньше запуска службы»;
   - `CounterState{Version, Points map[key]CounterPoint}` сериализуется в JSON; интерфейс `model.CounterStateStore{Load(profileID), Save(profileID, state)}`; в фазе 2 только `MemCounterStore` (потеря состояния = новый baseline). Файловое хранение и цикл опроса — **фаза 3** (`internal/monitor`).
10. Подключение: `github.com/microsoft/go-mssqldb`, логин/пароль только в `msdsn.Config` (не в строке DSN/логах/ошибках); `host\instance` без порта → SQL Browser; Windows Auth — `winsspi` (secur32.dll) на Windows, текущая учётка службы или `DOMAIN\user`+пароль. Ошибки — `*sqlcheck.Error{Kind conn|timeout|canceled|query|mapping|config}`; в статусе профиля ошибка соединения → `ProfileStatus.Error` (уровень Error).

**Checks:** `CHK-SQL-01` TestConnection/драйвер/опции подключения; `CHK-SQL-02` Discovery + DiscoverByValue на фикстурах схем; `CHK-SQL-03` ListPoints по mapping + режим counters; `CHK-SQL-04` ручной override / SQL-шаблон / валидация / preview. Интеграционные тесты (реальный SQL Server в linux-job CI, фикстуры `_Node123`+`_InfoRg456` со сдвигом 2000 и 0, `_Reference77`+`_InfoRg88` без `_Node`, `_Reference55` с датами, не-1С схема `exch.[Node]]Exchange]`) — по env `EC_SQL_TEST_*`.

```go
type ExchangeReader interface {
    Test(ctx) error
    Discover(ctx) ([]model.DiscoveryCandidate, error)
    DiscoverByValue(ctx, model.ValueQuery) ([]model.DiscoveryCandidate, error)
    ListPoints(ctx) ([]model.ExchangePoint, error)
}
```

Реализации: `StubReader`, `DiscoveringReader` (таблица / linked / counters), `QueryReader` (ручной SQL); `ReaderFor`/`NewReader` выбирают по профилю; `Status(ctx, r, p, now)` → `model.ProfileStatus`.

### 5.4. Кнопка «Тест» в GUI

Трей → IPC `TestProfile(profileID)` → служба выполняет `ExchangeReader.Test` (или трей может тестировать сам через тот же `internal/sqlcheck` — **решение:** тест делает **служба**, чтобы один стек драйвера/таймаутов; трей только показывает OK/ошибку).

---

## 6. IPC: постоянная связь служба ↔ трей

### 6.1. Транспорт

**Named pipe** (Win32 `CreateNamedPipeW` / `ConnectNamedPipe` / ReadFile/WriteFile):

- имя: `\\.\pipe\ExchangeChecker`
- один сервер = служба; клиент = трей
- framing: length-prefixed JSON messages (UTF-8)
- после обрыва трей переподключается с backoff; служба принимает нового клиента
- «постоянно» = держим соединение открытым; heartbeat `Ping` каждые N секунд

Альтернатива localhost TCP через `internal/hcwinnet` — запасной план, если named pipe на NT 6.1 упрётся в ограничения; в v1 целимся в pipe.

### 6.2. Протокол (черновик сообщений)

Служба → трей:

- `Hello` / `Pong`
- `StatusSnapshot` — полный список `ExchangePointStatus`
- `StatusDelta` — изменения
- `AlertRaised` { level, profileID, pointName, text, sound }
- `AlertCleared` { … }
- `ConfigReloaded` { ok/error }

Трей → служба:

- `Hello` / `Ping`
- `ReloadConfig`
- `TestProfile` { profileID } → ответ `TestResult`
- `GetStatus`
- `AckAlert` { alertID } — пользователь закрыл модалку → служба может остановить политику звука на своей стороне (звук играет трей)

### 6.3. Кто показывает UI алерта и кто пищит

- Решение о warning/error принимает **служба** (`monitor` + `alert`).
- **Трей** по `AlertRaised` показывает модальное окно поверх всех (`HWND_TOPMOST` + `MB_SYSTEMMODAL` / собственный dialog) и запускает **бесконечный** цикл `Beep` (WinAPI `Beep` / `MessageBeep` — предпочтительно `kernel32!Beep` с частотой из настроек) в отдельной goroutine до `AckAlert` / закрытия окна.
- Если трей не запущен: служба пишет уровень в `state.json` и при появлении трея сразу шлёт накопленные алерты (очередь).

---

## 7. Поведение UI трея

### 7.1. Иконка

- ЛКМ по иконке → popup/окно списка **баз (профилей)**: строка базы **жёлтая** при warning хотя бы по одной точке, **красная** при error; точки обмена в корне **не** показываются; при **наведении** на базу — выпадающее меню со всеми точками этой базы (LastSend, LastRecv, FreshAt, уровень).
- ПКМ → меню: **Настройки** | **О программе** | (позже) Выход.

### 7.2. Настройки

Вкладки/секции (простой Win32, без «наворотов»):

1. **Профили** — список, добавить/изменить/удалить; поля host, port, database, user, password; кнопка **Тест**.
2. **Мониторинг** — PollInterval, WarningAfterMin, ErrorAfterMin.
3. **Звук** — частоты/длительности warning и error, gap.
 4. **Язык** — Русский / English (полный UI).

Сохранение → файлы + `ReloadConfig` службе.

### 7.3. О программе

Имя продукта, версия (из `internal/version`), краткий копирайт.

### 7.4. Язык UI (принято)

- Полная локализация **русского и английского** (меню, окна, ошибки, алерты, инсталлятор).
- Переключение в настройках; оба языка — 100% покрытия UI.
- **Язык по умолчанию при первом запуске** = язык UI Windows (OS). Если язык ОС — ни русский, ни английский, fallback = **RU**. Пользователь всегда может переключить RU/EN в настройках.


---

## 8. Служба Windows

- Регистрация через инсталлятор API SCM (`CreateServiceW` и т.д.), **не** через `sc.exe`.
- Тип: own process, auto start — если галочка на «Готово».
- `ServiceMain` → запуск IPC-сервера + `monitor` loop.
- Корректный stop/shutdown: отмена `context`, закрытие pipe и SQL.

Цикл монитора:

```text
load config
for each enabled profile:
  ListPoints
  evaluate lags vs thresholds
  update snapshot
  emit AlertRaised/Cleared as needed
broadcast StatusSnapshot/Delta
sleep PollIntervalSec (прерываемый)
```

---

## 9. Инсталлятор

Wizard Win32 (страницы):

1. Приветствие  
2. Папка установки (default ` %ProgramFiles%\ExchangeChecker\`)  
3. Опции: ярлык в меню Пуск; **галочка «Запустить службу после установки»** (на финише «Готово»)  
4. Прогресс копирования файлов  
5. Готово  

Действия:

- копировать `ExchangeCheckerSvc.exe`, `ExchangeCheckerTray.exe`, ресурсы;
- создать Uninstall-ключ в Registry + `Uninstall.exe` или тот же setup с `/uninstall`;
- зарегистрировать службу;
- опционально стартовать службу (`StartServiceW`);
- **автозапуск трея** при входе пользователя: запись в автозагрузку пользователя (HKCU Run через WinAPI) — **обязательно** в v1.

Деинсталлятор: остановить/удалить службу, убрать Run, удалить файлы и ключи Uninstall, сохранить или спросить про `%ProgramData%\ExchangeChecker` (по умолчанию настройки оставляем — безопаснее).

---

## 10. Автотесты и check ID

Файл `checks/CHECKS.md` (или `checks/manifest.json`) — реестр. **Без check ID мерж фичи запрещён** (`POLICY.md`).

Предлагаемый каркас ID:

| Check ID | Что покрывает |
|----------|----------------|
| `CHK-POL-01` | Политики репо: go 1.20 в go.mod, нет запрещённых вызовов helper-процессов (static scan) |
| `CHK-CFG-01` | Чтение/запись settings/profiles, DPAPI roundtrip |
| `CHK-SQL-01` | TestConnection (mock + optional integration) |
| `CHK-SQL-02` | Discovery: выбор таблицы/полей на фикстуре схемы |
| `CHK-SQL-03` | ListPoints по сохранённому mapping |
| `CHK-SQL-04` | Ручной override mapping / SQL-шаблон |
| `CHK-MON-01` | Пороги warning/error на фикстурах времени |
| `CHK-IPC-01` | Pipe: connect, heartbeat, ReloadConfig, StatusSnapshot |
| `CHK-IPC-02` | AlertRaised → AckAlert |
| `CHK-SVC-01` | Каркас службы (start/stop в тестовом харнесе где возможно) |
| `CHK-TRAY-01` | Разбор меню/команд без GUI pump где unit-тестируемо; smoke GUI — отдельный runner |
| `CHK-SETUP-01` | Инсталлятор: dry-run/registry helpers unit; полный UI — manual/smoke |
| `CHK-DELIV-01` | Zip по DELIVERY.md без исходников/node_modules |

Каждый PR/коммит фичи ссылается на ID в сообщении коммита или в `checks/`.

---

## 11. Фазы внедрения

### Фаза 0 — Каркас репозитория (после ОКка плана)

- `go.mod` (go 1.20), `internal/version`, пустые `cmd/*`, `DELIVERY.md`, `checks/CHECKS.md`
- check: `CHK-POL-01`

### Фаза 1 — Config + модель ✅

- `internal/config`, `internal/model`, DPAPI-секреты (`CRYPTPROTECT_LOCAL_MACHINE`)
- CI: `.github/workflows/ci.yml` (Go 1.20.14, windows + linux)
- checks: `CHK-CFG-01`, `CHK-CFG-02`, `CHK-CI-01`
- Результаты CI читаются через GitHub Actions API; отдельная ветка отчётов и Actions artifacts не используются.

### Фаза 2 — SQL spike + Discovery ✅

- `internal/sqlcheck` + `ExchangeReader`: TestConnection, **Discover** (универсальный, без зависимости от `_Node*`), **DiscoverByValue**, ListPoints по mapping (single / linked / counters), ручной mapping и SQL-шаблон с валидацией и preview
- `internal/model`: опции подключения профиля, расширенный Mapping, `DiscoveryCandidate`/`ValueQuery`/`CounterState` (IPC-ready JSON), `ApplyCounters`, `CounterStateStore` (+ in-memory)
- драйвер `github.com/microsoft/go-mssqldb v1.8.2` (§5.2); CI: linux-job поднимает `mcr.microsoft.com/mssql/server:2019-latest` для интеграционных тестов; windows-job проверяет отсутствие `os/exec` в графе зависимостей
- GUI «Найти таблицы» / подтверждение кандидата — в фазе 6 (трей) через IPC (фаза 4); файловое хранение состояния счётчиков и цикл опроса — фаза 3
- checks: `CHK-SQL-01` … `CHK-SQL-04`


### Фаза 3 — Monitor + alert logic

- `internal/monitor`, `internal/alert` без UI
- check: `CHK-MON-01`

### Фаза 4 — IPC

- `internal/ipc` named pipe, протокол
- checks: `CHK-IPC-01`, `CHK-IPC-02`

### Фаза 5 — Служба

- `cmd/exchsvc` + `internal/winsvc`, цикл + IPC server
- check: `CHK-SVC-01`

### Фаза 6 — Трей

- `cmd/exchtray` + `internal/wintray`: меню, настройки, статус, модалки, Beep
- check: `CHK-TRAY-01`

### Фаза 7 — Инсталлятор

- `cmd/exchsetup` + `internal/wininstall`
- check: `CHK-SETUP-01`

### Фаза 8 — Поставка

- `DELIVERY.md` + скрипт упаковки **в среде нейронки** (не у заказчика)
- check: `CHK-DELIV-01`
- Сборка zip — **только по явной просьбе**

Параллелизация ботами после старта кода: **EC Service**, **EC Tray**, **EC Setup**, **EC SQL** (spike), под координацией Вики; всем один `POLICY.md`.

---

## 12. Решения заказчика и остаток вопросов

### Принято

1. SQL-схема: авто-Discovery (§5.3), подтверждение mapping в GUI; ручной SQL — fallback.
2. **Алерт по одной «свежей» дате:** `FreshAt = max(LastSend, LastRecv)`; лаг от FreshAt к «сейчас» (UTC+3). Отдельные пороги на send/recv не используются.
3. **Время в БД — UTC+3, точно.**
4. **SQL Authentication и Windows Authentication** — оба в v1.
5. **Автозапуск трея** вместе с входом пользователя — да.
6. **UI:** русский и английский, полностью.
7. **ЛКМ-список:** только базы; цвет базы жёлтый (warning) / красный (error) по худшему уровню точек; точки обмена — во **выпадающем меню при наведении** на базу.
8. **Язык по умолчанию при первом запуске = язык ОС Windows** (если не RU/EN — fallback RU).

9. **DPAPI LocalMachine (фаза 1):** `CryptProtectData` с флагами `CRYPTPROTECT_LOCAL_MACHINE` (0x4) и `CRYPTPROTECT_UI_FORBIDDEN` (0x1), опциональный entropy `ExchangeChecker/secrets.dpapi/v1`. Трей (пользователь) шифрует, служба под LocalSystem расшифровывает. Граница доступа — ACL каталога `%ProgramData%\ExchangeChecker\` (инсталлятор, фаза 7: Administrators+SYSTEM+Users-read). Tradeoff v1: любой локальный процесс на машине может расшифровать blob — принято как допустимое для v1.

10. **Сеть SQL (фаза 2):** драйвер использует собственный `net.Dialer` (TCP, `dial timeout`); общего сетевого пакета/конвенции в проекте пока нет (`internal/hcwinnet` — заглушка). Логин/пароль задаются только полями `msdsn.Config`, в DSN/логи/ошибки не попадают.
11. **`Encrypt ""` = умолчание драйвера** (TLS только для логина, без проверки сертификата) — максимальная совместимость с SQL 2005/2008; для старых серверов без TLS 1.2 — `tlsMinVersion "1.0"` или `encrypt "disable"`.
12. **Discovery не зависит от `_Node*`** (решение архитектора): `_Node*` даёт только +10; универсальный путь по `_Reference*`/`_InfoRg*`/`_Document*`/`_Node*` и не-1С таблицам; `DiscoverByValue` — поиск по имени известной точки (+ примерное время).
13. **Только чтение:** все запросы Discovery и опроса — `READ UNCOMMITTED`/`WITH (NOLOCK)`, `TOP (@p1)`, параметры, экранированные имена; бюджеты времени и лимиты таблиц/строк (§5.3). Грязное чтение для мониторинга допустимо; блокировать 1С нельзя.
14. **Linked-опрос — `INNER JOIN`:** точки справочника без строки в таблице времён (например, «этот узел» плана обмена) не показываются; опция `keepUnlinkedPoints` → `LEFT JOIN` (такие точки = Error `no_timestamps`).
15. **Пустая дата 1С:** год < 1900 после вычета `YearOffset` (`2001-01-01` при сдвиге 2000, `1753-01-01` без сдвига) → NULL. `YearOffset` только 0 или 2000.
16. **Типы для IPC в `internal/model`:** `DiscoveryCandidate`, `SampleRow`, `ValueQuery`, `CounterState`/`CounterPoint`/`CounterTimes`, интерфейс `CounterStateStore` — трей не импортирует драйвер. `sqlcheck.DiscoveryCandidate` — alias.
17. **Ошибка соединения → `ProfileStatus.Error`** (уровень Error, фаза 1): отдельного уровня ConnError нет; тип ошибки — `sqlcheck.KindOf(err)` (conn/timeout/canceled/query/mapping/config).
18. **ManualSQL** выполняется в транзакции `READ UNCOMMITTED`, которая всегда откатывается; допускается ровно один `SELECT`/`WITH`, ровно 3 колонки; при заданных и таблице, и ManualSQL побеждает ManualSQL.
19. **Режим `counters`** (решение архитектора через Вику): fallback для конфигураций без хранимых времён; время = момент, когда служба увидела изменение `_SentNo`/`_ReceivedNo`; первое наблюдение после старта — baseline (`sendBaseline`/`recvBaseline`, UI: «не раньше запуска службы»); **уменьшение/сброс счётчика считается изменением**; одна таблица `_Node*` на mapping; в кандидате Discovery колонки счётчиков передаются в `SendCol`/`RecvCol` (→ `sentNoColumn`/`receivedNoColumn`), score фиксированно низкий (5). Хранилище состояния: интерфейс `model.CounterStateStore`, в фазе 2 — только `MemCounterStore`; файл и цикл опроса — фаза 3 (`internal/monitor`).
20. **Интеграционные тесты SQL** — в linux-job CI (контейнер `mcr.microsoft.com/mssql/server:2019-latest`, env `EC_SQL_TEST_HOST/PORT/USER/PASSWORD`); без env тесты пропускаются (windows-job).

### Ещё желательно (не блокер каркаса)

- Доступ к одной тестовой MSSQL для калибровки Discovery.


---

## 13. Риски

| Риск | Митигация |
|------|-----------|
| Неизвестная схема таблиц 1С | Авто-Discovery + подтверждение mapping в GUI; ручной SQL как fallback; калибровка эвристик на тестовой БД |
| PC speaker на современных ПК без спикера | Beep всё равно зовём; параллельно можно дублировать `MessageBeep` — не вместо, а опция |
| Трей не запущен во время аварии | Очередь алертов в службе; при старте трея — повторная доставка |
| Go 1.21+ соблазн | Запрет POLICY; CI/check на версии в go.mod |
| Помощники sc/powershell | Только WinAPI SCM/Registry; static scan `CHK-POL-01` |
| Ложные кандидаты Discovery (курсы валют, периодические регистры, документы со свежими датами) | Ранжирование по доле обновлённых точек и двум сериям; hints/Reason в GUI; подтверждение пользователем; `DiscoverByValue`; ручной mapping |
| SQL 2005/2008 и TLS 1.0 | `tlsMinVersion "1.0"` / `encrypt "disable"`; на реальном legacy-сервере не проверено (в CI — SQL 2019) |
| Режим counters: время = момент наблюдения, не реальное событие | Флаги baseline; точность = период опроса; сохранение состояния в фазе 3 |

---

## 14. Что не делаем в v1

- Веб-морда, Electron, WPF, tray на сторонних UI-kit
- Уведомления по почте/Telegram
- Кластер из нескольких машин-агентов
- Сборка `.exe`/zip без явной просьбы заказчика
