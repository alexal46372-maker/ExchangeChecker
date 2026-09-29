# PLAN.md — детальный план Exchange Checker

Статус: решения §12 п.2–9 **приняты** (см. §12). SQL Discovery — §5.3. **Фазы 0, 1, 2 (2b, 2c, 2d) и 3 (3b) выполнены** (фаза 3 — монитор, алерты, IPC-сервер и обёртка службы; 3b — устойчивость: сбой конфигурации, лимиты IPC, зависший ридер, §11). Язык UI по умолчанию при первом запуске = язык ОС Windows (если не RU/EN — fallback RU).

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
| `state.json` | последний снимок статусов (опциональный кэш для трея при старте до ответа службы) — **в v1 не используется**: трей получает снимок по IPC сразу после подключения | служба |
| `counters-state.json` | состояние режима `counters` по всем профилям (фаза 3, `monitor.FileCounterStore`, атомарная запись) | служба |
| `logs\exchsvc.log` | журнал службы с ротацией по размеру (1 МиБ × 5, `alert.RotatingFile`) | служба |

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
  WarnMinutes, ErrorMinutes int // фаза 3: override порогов на профиль (0 = глобальные 30/60)
  ServerPreset string          // новые профили (model.NewProfile) — "mssql2008"
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
   - TLS (с фазы 2d, решение заказчика — §12 п. 31): `Encrypt` "" (поле не задано) = **`disable`** (`EffectiveEncrypt()`), `optional` — TLS только для логина (умолчание драйвера, сертификат не проверяется), `disable` — без TLS, `on` (+`TrustServerCert`), `strict` (TDS 8, SQL 2022+). Go 1.20 по умолчанию требует TLS ≥ 1.2: для SQL 2005/2008 без TLS 1.2 — `TLSMinVersion "1.0"` или `Encrypt "disable"`.
   - **Целевой сервер заказчика — только SQL Server 2008** (возможно R2; считаем уровень совместимости **100**, ничего из 2008 R2 не используем) — §12 п. 21. Все запросы службы (построители `sqlbuild.go`, `select 1;` драйвера при Ping, `sp_executesql` для параметров, транзакции через TDS TransMgrReq) и шаблоны ручного SQL (`sqlcheck.ManualSQLTemplates`) — T-SQL 2005/2008: `TOP (@p)`, `COUNT_BIG`, `CASE`, `ROW_NUMBER() OVER (PARTITION BY … ORDER BY …)`, `INFORMATION_SCHEMA`, `sys.partitions`/`sysindexes`, `WITH (NOLOCK)`. Запрещено (2012+): OFFSET/FETCH, TRY_CONVERT/TRY_CAST/TRY_PARSE/PARSE, IIF, CHOOSE, CONCAT(_WS), FORMAT, STRING_AGG/STRING_SPLIT, TRIM, EOMONTH, `*FROMPARTS`, THROW, LAG/LEAD/FIRST_VALUE/LAST_VALUE/PERCENTILE_*, агрегаты с `OVER (… ORDER BY …)` и рамки ROWS/RANGE, `sys.dm_*`, JSON-функции. Проверка — `sqlcheck.Compat2008Violations` (CHK-SQL-05, все построители + Discovery + DiscoverByValue + шаблоны); ручной SQL с такими конструкциями отклоняется (`ErrManualCompat2008`, подсказка: TOP / ROW_NUMBER() / CASE / CONVERT / `+`). Интеграционные базы в CI создаются с `COMPATIBILITY_LEVEL = 100`.
   - **Пресет «SQL Server 2008»:** `Profile.ServerPreset "mssql2008"` (`omitempty`) и `Profile.ApplyServerPreset("mssql2008")`: `Encrypt "disable"`, если не задан (фаза 2d, §12 п. 31; явные `optional`/`on`/`disable` сохраняются), `TLSMinVersion "1.0"` (если не задан явно; `EffectiveTLSMinVersion()` даёт "1.0" и без явного поля; действует, только если шифрование включено), `strict` → `disable`; `strict` + пресет не проходит `Validate`. В go-mssqldb v1.8.2 `tlsmin=1.0` → `tls.Config.MinVersion = tls.VersionTLS10` (выставляется явно: клиент Go 1.20 без этого требует TLS 1.2). Подсказка в UI — ключ `sql.preset_mssql2008_hint` («без обновлений TLS 1.2 нужен TLS 1.0»).
   - **Ошибка TLS-рукопожатия** классифицируется (`KindConn` + `sqlcheck.Error.Key`, `ProfileStatus.ErrorKey`, `omitempty`): `sql.tls_enable_tls10` — «включите TLS 1.0 в профиле» (пресет или «Минимальная версия TLS: 1.0»); `sql.tls_legacy_failed` — TLS 1.0 уже разрешён, общего протокола/шифра нет (обновление TLS 1.2 на сервере или `encrypt disable`); `sql.tls_certificate` — сертификат отклонён (x509). Тексты RU/EN — `sqlcheck.Message(key, lang)`, `MessageKeys()`. Каталога сообщений в фазе 1 нет; конвенция — язык `ru`/`en` (`model.LangRU/LangEN`, `config.EffectiveLanguage`, fallback RU), поэтому sqlcheck отдаёт стабильные ключи, а трей (фаза 6) показывает текст. Драйвер возвращает `fmt.Errorf("TLS Handshake failed: %v", err)` (причина только текстом), поэтому классификация текстовая + `tls.RecordHeaderError`.
   - **Что на самом деле значит `encrypt` в go-mssqldb v1.8.2** (код `msdsn/conn_str.go` `parseTLS`, `session.go` `preparePreloginFields`, `tds.go` `connect`; подтверждено тестом `TestCHK_SQL_05_IntegrationEncryptWire` — TCP-прокси записывает байты клиент→сервер):
     - `disable` → `msdsn.EncryptionDisabled`, `TLSConfig = nil`, в prelogin клиент объявляет `ENCRYPT_NOT_SUP`. Если сервер не форсирует шифрование — **TLS нет ни для данных, ни для логина**: пакет Login7 (логин, имя хоста/приложения, пароль) идёт открытым текстом; пароль SQL лишь «замаскирован» (перестановка полубайтов + XOR 0xA5, тривиально обратимо) — **фактически открытый текст**; при Windows-аутентификации вместо пароля идут SSPI-токены (NTLM/Kerberos). Если сервер форсирует шифрование (`ForceEncryption=Yes`), v1.8.2 не обрывает соединение, а выполняет рукопожатие с настройками Go по умолчанию (TLS ≥ 1.2, проверка сертификата; наш `tlsmin` не применяется) — на SQL 2008 без TLS 1.2 это не соединится, нужен пресет (умолчание + TLS 1.0).
     - "" (с фазы 2d) → как `disable`.
     - `optional` (до фазы 2d — также "") → `EncryptionOff`, prelogin `ENCRYPT_OFF`: TLS-рукопожатие, **в TLS уходит только Login7** (пароль защищён), затем канал возвращается в открытый режим — запросы и результаты читаемы в сети; сертификат не проверяется; `tlsmin` применяется к этому рукопожатию. Если у сервера нет TLS (`ENCRYPT_NOT_SUP`) — логин открытым текстом.
     - `on` → вся сессия в TLS (`TrustServerCert` отключает проверку сертификата); `strict` — TDS 8.0, только SQL 2022+.
   - Таймауты: `dial timeout` = ConnectTimeoutSec; остальное — через context (QueryTimeoutSec на запрос); `connection timeout` драйвера (per-IO deadline) не используется.
2. Подключение: host/port/db; **SQL Authentication и Windows Authentication — оба обязательны** в v1 (выбор AuthMode в профиле).
3. Запрос времён — **сначала авто-Discovery + сохранённый mapping** (§5.3); ручной SQL-шаблон в профиле — запасной режим для нестандартных баз.

### 5.3. Автопоиск таблицы и полей (основной способ)

Заказчик **не обязан** знать имена таблиц 1С. Софт сам ищет кандидатов в указанной базе и предлагает выбор в GUI («Найти таблицы» / при первом «Тест»).

**Алгоритм Discovery (служба, `internal/sqlcheck/discover`):** (исходный набросок)

1. `TestConnection` — логин в MSSQL успешен.
2. Читаем каталог и отбираем кандидатов эвристиками; для кандидата подбираем `PointName`, `LastSend`/`LastRecv`; считаем score; топ-N отдаём трею; пользователь подтверждает; опрос — только по сохранённому mapping; ручной SQL — запасной путь.

**Реализация (фаза 2, `internal/sqlcheck`; фаза 2b — ниже).** Универсальный путь работает для любой конфигурации 1С (планы обмена, самописные справочники+регистры, даты прямо в справочнике) и для не-1С схем; **с фазы 2b планы обмена `_NodeNNN` — основной источник** (бонус +40, несколько планов в профиле, §12 п. 22–25). Все запросы — **только чтение**: `SET TRANSACTION ISOLATION LEVEL READ UNCOMMITTED` в каталожных батчах + `WITH (NOLOCK)` на каждой пользовательской таблице, `TOP (@p1)` везде, имена — только через `QuoteIdent` (`[...]`, `]`→`]]`, длина ≤128), значения — только параметрами.

**Алгоритм Discovery (`Discover`):**

1. Каталог: `INFORMATION_SCHEMA.COLUMNS` + `INFORMATION_SCHEMA.TABLES` (`BASE TABLE`, одним запросом). Число строк: `sys.partitions` (index_id 0/1), fallback — `sysindexes` (SQL 2005 без прав на DMV); нет ни того ни другого — работаем без row counts (лимиты по TOP).
2. **Справочники точек** — таблицы `_Reference*`, `_Node*`, `_InfoRg*`, `_Document*` и не-1С таблицы с ≤ `MaxDirRows` (5000) строк, у которых есть строковая колонка имени (`_Description`, иначе `_Code`, иначе строковая колонка ≤ 4000 символов / маркеры name/node/point/узел).
3. **Источники кандидатов:**
   - *same_table* — две (или одна) datetime-серии в самом справочнике (`_Reference55._Fld56/_Fld57`, база 35);
   - *linked* — таблица времён (`_InfoRg*`/`_Document*`/любая) с колонкой `binary(16)`, чьи значения совпадают с `_IDRRef` справочника (выборка до 500 ссылок → доля совпадений и покрытие точек; база 35 + ratio·10 + coverage·5, `_InfoRg` +2);
   - *generic* — не-1С таблица с именем точки и datetime-колонками (маркеры exchange/node/sync/send/recv…, база 25);
   - *counters* — **fallback** для конфигураций без хранимых времён: при наличии `_Node*` с `_SentNo`/`_ReceivedNo` кандидат с фиксированным **низким** score 5, hint `counters_fallback`, Reason объясняет, что это запасной режим.
   - Бонус `_Node*` — ~~только +10~~ **+40** (`nodeBonus`, hint `node_table`, фаза 2b); не условие: без `_Node` универсальный путь работает как раньше. Составные ссылки 1С 8.1 (`_TYPE`/`_RTRef`/`_RRRef`) засчитываются только при `_TYPE = 0x08` и `_RTRef` = номер таблицы-владельца (hint `composite_ref`).
4. **Серии времени** (`evaluateSeries`): MAX/COUNT по каждой datetime-колонке (до 16 колонок), поиск `YearOffset` (MAX год > now+1000 → 2000), свежесть MAX после сдвига: ≤1ч +30, ≤1д +25, ≤7д +18, ≤30д +10, ≤1г +4, иначе +1; «будущее» −10; две серии +10; send/recv по именам (+5, частично +2), иначе две самые свежие по порядку колонок (hint `send_recv_by_order` — подтвердить в GUI).
5. **Ранжирование** (`rank`, топ-30): реальный ListPoints по кандидату: +15·(доля точек, обновлённых в пределах 7 дней от freshest), +5·(доля точек с обеими датами), +5 за 2..500 точек (hint `all_points_fresh`).
6. **Бюджеты:** общий `Timeout` 90 с, `QueryTimeout` 15 с на запрос, `MaxDirTables` 300, `MaxTimesTables` 400, `StatsRowLimit` 200 000, `MaxLinkedRows` 500 000, `MaxLinkTables` 200. Ошибка/таймаут одной таблицы — таблица пропускается; исчерпан общий бюджет — частичный результат + hint `truncated`; отмена родительского ctx — ошибка.

**`DiscoverByValue(ValueQuery{PointName, ApproxTime?, ToleranceMin})`** — пользователь вводит имя знакомой точки (и, опционально, примерное время последнего обмена):

1. `LIKE N'%…%'` (экранирование `[`, `%`, `_` → литералы) по строковым колонкам (≤ 24 на таблицу, ≤ 1024 символов) таблиц с ≤ `MaxValueRows` (100 000) строк, ≤ 300 таблиц, `MatchRows` 20 на таблицу.
2. Даты берутся из **той же строки** или из **связанных строк** других таблиц по `binary(16)`-ссылке (`WHERE ref = @p2`, ≤ `MaxMatches` 20 совпадений).
3. Score: база 50; точное совпадение +15 (hint `exact_match`) / подстрока +5; близость ближайшей даты к ApproxTime ≤ 1 ч +30, ≤ tolerance +20 (обе — hint `near_approx_time`), ≤ 7 дн +8; без ApproxTime — свежесть; `_Node` ~~+10~~ +40 (фаза 2b), `_InfoRg` +2. Результат — те же `DiscoveryCandidate` + `Matched` (найденная строка).
4. **Фаза 2b:** при заданном `ApproxTime` сначала работает **поиск по времени** (ниже); поиск по имени — запасной путь, если по времени кандидатов нет.

**Фаза 2b — планы обмена как основной источник (уточнение заказчика):**

- **`_NodeNNN` — основной источник:** `nodeBonus` = **+40** в Discovery и `DiscoverByValue`; на фикстуре с двумя планами, курсами валют и коллизией ссылок кандидаты планов опережают любые не-`_Node` кандидаты с запасом ≥ 30 (тест). Кандидат несёт `planTable`/`planNo` (`_Node124` → 124).
- **Несколько планов в одном профиле:** `Profile.Mappings []Mapping` — дополнительные mapping к основному `Mapping` (`omitempty`, профили фазы 1/2 читаются без изменений); у каждого `planLabel` (подпись в UI). `ListPoints` обходит `Profile.AllMappings()` (основной + дополнительные; у дополнительных ManualSQL побеждает таблицу), каждая точка получает `planTable`/`planNo`/`planLabel`. Наблюдения счётчиков всех counters-mapping применяются к состоянию профиля одним шагом; ключи дополнительных mapping префиксуются `Table + "/"` (точки разных планов не смешиваются). `model.SuggestPlanMappings(cands)` — лучший не-counters кандидат на каждую таблицу `_Node*` (GUI «добавить все планы»). Дополнительные mapping требуют настроенного основного, каждый проходит `Validate`.
- **Составные ссылки 1С 8.1** (`_FldNNN_TYPE` binary(1) + `_FldNNN_RTRef` binary(4) + `_FldNNN_RRRef` binary(16)): без фильтра строки, ссылающиеся на объект **другой** таблицы с тем же 16-байтным ключом, дали бы ложные даты. Mapping: `timesLinkTypeColumn`, `timesLinkRTRefColumn`, `timesLinkRTRef` (номер таблицы 1..2³¹−1; 4 байта big-endian); SQL-фильтр — параметры `@p2 = 0x08`, `@p3 = RTRef` внутри подзапроса таблицы времён (ListPoints, LinkStats, поиск по значению — `buildCompositeSearchSQL`). `CheckMapping` проверяет типы binary(1)/binary(4).
- **`DiscoverByValue` по времени** (`ValueQuery{PointName?, PointRef?, PlanTable?, ApproxTime, ToleranceMin}`): точку задают по `_IDRRef` (`pointRef`, 32 hex, выбор из `ListNodePoints`), либо по имени/коду (опционально только в `planTable`); точные совпадения предпочтительнее. Ищутся datetime-колонки в той же строке `_Node` и в таблицах, ссылающихся на точку (простая ссылка или составная с RTRef = номер плана; ≤ `MaxLinkedRows`, ≤ `MaxLinkTables`); MAX по колонке после YearOffset → колонки в пределах tolerance; близость: ≤ 15 мин +35, ≤ 1 ч +30, ≤ 6 ч +25, иначе +20; база 60 + серии + `nodeBonus`; hints `by_value`, `by_time`, `near_approx_time`. Нет попаданий по времени → поиск по имени (с фильтром составных ссылок). Неверный `pointRef` → `ErrBadPointRef` (KindConfig).
- **`ListNodePoints` / `PlanPointLister.NodePoints(ctx)`:** строки всех `_Node*` (`planTable`, `planNo`, `ref` hex, `code`, `name`) для выбора точки в трее.

**Типы (IPC-ready JSON, пакет `internal/model`, без драйвера):**

```go
DiscoveryCandidate { Mode single|linked|counters, Schema, Table, PointCol, PointIDCol, PointKeyCol,
    SendCol, RecvCol, TimesSchema, TimesTable, LinkCol, LinkTypeCol, LinkRTRefCol, LinkRTRef,
    PlanTable, PlanNo, YearOffset, FreshestAt, Points,
    Matched *SampleRow, SampleRows []SampleRow, Score, Reason, Hints []string }  // .Mapping()
ValueQuery { PointName, PointRef, PlanTable, ApproxTime *time.Time, ToleranceMin } // default 1440
NodePoint { PlanTable, PlanNo, Ref, Code, Name }                                  // ListNodePoints
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
  TimesLinkTypeColumn, TimesLinkRTRefColumn string; TimesLinkRTRef int   // 2b: составная ссылка (_TYPE = 0x08, _RTRef = N)
  PlanLabel          string   // 2b: подпись плана обмена в UI (planTable/planNo выводятся из Table)
  KeepUnlinkedPoints bool     // LEFT JOIN вместо INNER (точки без строки времён → no_timestamps)
  LastSendColumn, LastRecvColumn string
  YearOffset         0 | 2000 // сдвиг лет 1С
  SentNoColumn, ReceivedNoColumn string  // counters: по умолчанию _SentNo/_ReceivedNo
  ManualSQL          string
}
Profile: + Encrypt ""|optional|disable|on|strict, TrustServerCert, TLSMinVersion ""|1.0|1.1|1.2,
           ConnectTimeoutSec (15), QueryTimeoutSec (30),
           ServerPreset ""|mssql2008 (2b), Mappings []Mapping (2b: дополнительные планы обмена)
```

7. Рабочий опрос (`ListPoints`) идёт **только по сохранённому mapping**, без повторного угадывания (тоже `NOLOCK`). Даты: naive → минус `YearOffset` → `FromDBNaive` (UTC+3); год < 1900 после сдвига (пустая дата 1С `2001-01-01`/`1753-01-01`) → NULL; обе NULL → Error `no_timestamps` (решение фазы 1). Linked: `INNER JOIN` + `MAX()` по точке (точки без строки времён пропускаются; `keepUnlinkedPoints` → `LEFT JOIN`).
8. **Ручной mapping / SQL-шаблон — полноценный режим (CHK-SQL-04):** `ValidateMapping` (таблицы/колонки существуют, типы: datetime для времён, строка/число для имени, `binary` одинаковой длины для связи, числа для счётчиков; неоднозначное имя таблицы без схемы → ошибка), `PreviewMapping` (первые N строк), понятные ошибки `ErrTableNotFound`/`ErrTableAmbiguous`/`ErrColumnNotFound`/`ErrColumnType`/`ErrLinkTypeMismatch`/`ErrNoTimestampColumn` с `schema.table.column`. `ManualSQL`: ровно один `SELECT`/`WITH` (комментарии и литералы учитываются, `;` только в конце; **фаза 2c:** ровно один SELECT верхнего уровня, кроме UNION/EXCEPT/INTERSECT, и запрещённые ключевые слова вне литералов/комментариев/`[имён]`/`"имён"` — §12 п. 28), выполняется на отдельном соединении пула в транзакции `READ UNCOMMITTED`, которая **всегда откатывается**, после чего сессия возвращается в `READ COMMITTED` (§12 п. 29); ровно 3 колонки (имя, send, recv); `YearOffset` применяется к результату. Если заданы и таблица, и ManualSQL — используется ManualSQL (`QueryReader`).
9. **Режим `Mode="counters"`** (решение архитектора, через Вику) — для конфигураций, где времена обмена не хранятся вообще:
   - `ListPoints` читает `_SentNo`/`_ReceivedNo` (+ `_Code`/`_Description`/`_IDRRef`) из одной таблицы `_Node*` (`NOLOCK`, имена экранированы, `TOP (@p1)`);
   - чистая функция `model.ApplyCounters(prev CounterState, observed []CounterObservation, now) (next CounterState, []CounterTimes)`: новая точка → время = now, флаги baseline; счётчик не изменился → время и флаг сохраняются; **любое** изменение (в т.ч. **уменьшение/сброс** — 1С пересоздал узел или сбросил нумерацию) → время = now, baseline снимается; исчезнувшие точки удаляются из состояния; дубликаты ключа — берётся первый; now приводится к UTC+3;
   - ключ точки: hex `_IDRRef`, иначе `code:`/`name:`;
   - `ExchangePoint` получает `source:"counters"`, `sendBaseline`/`recvBaseline` (`omitempty`, обратно совместимо) — UI пишет «не раньше запуска службы»;
   - `CounterState{Version, Points map[key]CounterPoint}` сериализуется в JSON; интерфейс `model.CounterStateStore{Load(profileID), Save(profileID, state)}`; в фазе 2 только `MemCounterStore` (потеря состояния = новый baseline). **Фаза 3:** `monitor.FileCounterStore` — `%ProgramData%\ExchangeChecker\counters-state.json` (`{version, profiles: {id: CounterState}}`), каждая запись атомарна (`config.WriteFileAtomic`: temp-файл + fsync + `MoveFileExW(MOVEFILE_REPLACE_EXISTING|MOVEFILE_WRITE_THROUGH)`, вне Windows — rename); после перезапуска службы состояние восстанавливается, baseline не сбрасывается; ключи дополнительных планов — с префиксом `Table/`; повреждённый файл → пустое состояние + запись в журнал + копия `.corrupt`; ошибка записи не останавливает опрос (журнал, `LastError`); состояния удалённых профилей удаляются (CHK-MON-02).
10. Подключение: `github.com/microsoft/go-mssqldb`, логин/пароль только в `msdsn.Config` (не в строке DSN/логах/ошибках); `host\instance` без порта → SQL Browser; Windows Auth — `winsspi` (secur32.dll) на Windows, текущая учётка службы или `DOMAIN\user`+пароль. Ошибки — `*sqlcheck.Error{Kind conn|timeout|canceled|query|mapping|config}`; в статусе профиля ошибка соединения → `ProfileStatus.Error` (уровень Error).

**Checks:** `CHK-SQL-01` TestConnection/драйвер/опции подключения; `CHK-SQL-02` Discovery + DiscoverByValue на фикстурах схем (2b: приоритет `_Node`, составные ссылки, поиск по времени, ListNodePoints); `CHK-SQL-03` ListPoints по mapping + режим counters (2b: несколько планов, составные ссылки); `CHK-SQL-04` ручной override / SQL-шаблон / валидация / preview; `CHK-SQL-05` совместимость с SQL Server 2008 (compat 100), пресет, TLS 1.0 и ключи ошибок. Интеграционные тесты (реальный SQL Server в linux-job CI, все базы — `COMPATIBILITY_LEVEL = 100`; фикстуры `_Node123`+`_InfoRg456` со сдвигом 2000 и 0, `_Reference77`+`_InfoRg88` без `_Node`, `_Reference55` с датами, не-1С схема `exch.[Node]]Exchange]`, 2b: два плана `_Node123`/`_Node124` + регистр с составной ссылкой `_InfoRg600` и коллизией RTRef) — по env `EC_SQL_TEST_*`.

```go
type ExchangeReader interface {
    Test(ctx) error
    Discover(ctx) ([]model.DiscoveryCandidate, error)
    DiscoverByValue(ctx, model.ValueQuery) ([]model.DiscoveryCandidate, error)
    ListPoints(ctx) ([]model.ExchangePoint, error)
}
type PlanPointLister interface { NodePoints(ctx) ([]model.NodePoint, error) }   // 2b
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

### 6.2. Протокол

**Реализовано в фазе 3 (серверная сторона, CHK-MON-06)** — `internal/ipc`, типы в `internal/model/ipc.go` (трей не импортирует драйвер): кадр = 4 байта длины (little-endian) + JSON; служба → трей ≤ 16 МиБ, трей → служба ≤ 64 КБ (фаза 3b). Трей → служба `IPCRequest{id, cmd, profileId?}`: `ping`, `get_status`, `reload_config`, `poll_now` (последние два — не чаще раза в 10 с, иначе `ok` + `result: "accepted, coalesced"`; таймаут чтения 10 с, idle 5 мин, ≤ 8 клиентов — §12 п. 38). Служба → трей `IPCMessage{id, type, status?, alert?, error?}`: при подключении `hello` (version 1); ответы `pong` / `status` / `ok` / `error` с тем же `id`; push-сообщения (`id` = 0) — `status` после каждого опроса и `alert` на каждое подтверждённое событие. Снимок `StatusSnapshot{seq, now, level, profiles[{profileId, name, enabled, level, points[ExchangePoint + freshAt, lagMinutes, baseline], error, errorKey, freshAt, baseline, warnMinutes, errorMinutes, checkedAt, polled}], alerts (активные), pollIntervalSec, lastPollAt, configError}`, все времена — UTC+3. Pipe: `\\.\pipe\ExchangeChecker`, overlapped-I/O, `FILE_FLAG_FIRST_PIPE_INSTANCE`, `PIPE_REJECT_REMOTE_CLIENTS`, DACL `D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;0x12018b;;;IU)` (интерактивные пользователи — чтение/запись без права создавать экземпляры канала); медленный клиент (очередь 64) отключается, служба не блокируется. `ipc.Client`/`DialPipe` — минимальный клиент для трея. Ещё не реализовано (фазы 4/6): `TestProfile`, `AckAlert`, heartbeat-таймер на стороне трея.

Исходный черновик:

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

**Фаза 3:** события `AlertEvent{id, profileId, profileName, pointName, planTable, from, to, at, freshAt, lagMinutes, baseline, error, errorKey}` — переходы подтверждённого уровня точки (ok↔warning↔error) и профиля (ошибка соединения/запроса/таймаут). Антидребезг (`alert.Policy`): рост уровня точки — сразу (1 опрос), снижение — после 2 опросов подряд, ошибка профиля — после 2 опросов подряд; неизменный уровень событий не порождает. Приёмники: Windows Event Log (advapi32 `RegisterEventSourceW`/`ReportEventW`, источник `ExchangeChecker`, ID 100 служба, 101 восстановление, 102 warning, 103 error, 110 внутренняя ошибка), журнал службы, push по IPC; активные алерты — в каждом снимке (трей, подключившийся позже, видит их сразу — вместо очереди `state.json`).

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
- **Фаза 3 (CHK-MON-07):** `internal/winsvc` — `StartServiceCtrlDispatcherW` / `RegisterServiceCtrlHandlerExW` / `SetServiceStatus` через syscall + `syscall.NewCallback` (без golang.org/x/sys); START_PENDING → RUNNING (Stop+Shutdown) → STOP_PENDING (отмена ctx) → STOPPED; ошибка функции службы → `ERROR_SERVICE_SPECIFIC_ERROR`. Не из SCM → `ErrNotService`, `exchsvc` работает в консоли до Ctrl+C. Имя службы `model.ServiceName = "ExchangeChecker"` (для own-process SCM его не проверяет; инсталлятор регистрирует под этим именем).
- **Монитор (CHK-MON-03/04):** период — `settings.pollIntervalSec` (по умолчанию 60 с); все включённые профили параллельно, но не более 4 одновременно; случайная задержка старта профиля до min(2 с, период/10); у каждого профиля своя горутина, `recover` и бюджет времени `ConnectTimeout + QueryTimeout × (число mapping + 1)` — паника (`monitor.panic`), зависший драйвер (`monitor.timeout`, при следующем опросе `monitor.busy`) или ошибка одного профиля не задерживают остальные; ридеры (пулы соединений) переиспользуются между опросами и пересоздаются только при изменении профиля/пароля/порогов; остановка — отмена ctx, закрытие всех ридеров. Конфигурация перечитывается при изменении размера/mtime `settings.json`/`profiles.json`/`secrets.dpapi` (проверка `os.Stat` каждые 5 с, без fsnotify и потоков-наблюдателей) и по IPC `reload_config`; невалидные профили пропускаются поштучно, невалидные настройки → умолчания, ошибка — в `StatusSnapshot.configError` и журнале.

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
| `CHK-SQL-05` | Совместимость с SQL Server 2008 (compat 100): запрещённые конструкции, пресет, TLS 1.0 |
| `CHK-MON-01` | Статус: FreshAt, UTC+3, пороги warning/error (профиль/глобальные), худший уровень базы, ошибка = Error, baseline |
| `CHK-MON-02` | Файловое состояние counters: рестарт, повреждённый файл, атомарность |
| `CHK-MON-03` | Цикл опроса: изоляция профилей (panic/timeout), ограничение параллелизма, jitter, остановка |
| `CHK-MON-04` | Перечитывание конфигурации (mtime/size, IPC reload) |
| `CHK-MON-05` | События алертов: переходы, антидребезг, Event Log, журнал с ротацией |
| `CHK-MON-06` | IPC-сервер службы: протокол, named pipe, снимок статусов, команды |
| `CHK-MON-07` | Служба: SCM start/stop, интеграция монитора и IPC в `exchsvc` |
| `CHK-MON-08` | Пресет `mssql2008` по умолчанию для новых баз |
| `CHK-MON-09` | Интеграция с SQL Server: полный цикл counters + рестарт монитора |
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

### Фаза 2b — планы обмена, составные ссылки, SQL Server 2008 ✅

- уточнение заказчика: `_NodeNNN` — основной источник (+40), несколько планов в профиле (`Profile.Mappings`, `planLabel`), составные ссылки 1С 8.1, `DiscoverByValue` по времени (+ `pointRef`, `planTable`), `ListNodePoints`
- требование заказчика: целевой сервер только SQL Server 2008 (compat 100) — аудит всех запросов, `Compat2008Violations`, шаблоны ручного SQL, пресет `mssql2008` (TLS 1.0), ключи ошибок TLS (RU/EN); интеграционные базы CI с `COMPATIBILITY_LEVEL = 100`
- checks: `CHK-SQL-02` … `CHK-SQL-05`

### Фаза 2c — усиление безопасности SQL ✅

- `ValidateManualSQL`: обход без `;` закрыт (`SELECT 1,2,3 DELETE FROM t`, `SELECT … INTO`, `SELECT 1 COMMIT DELETE FROM t` — последний реально фиксировал транзакцию, подтверждено интеграционным тестом); запрещённые ключевые слова + ровно один SELECT верхнего уровня (§12 п. 28)
- уровень изоляции не «прилипает» к соединению пула: каталожные запросы — `WITH (NOLOCK)` вместо `SET TRANSACTION ISOLATION LEVEL`, после ручного SQL — явный возврат `READ COMMITTED` (§12 п. 29)
- учётная запись мониторинга — только `db_datareader` (§12 п. 30, DELIVERY.md)
- `go mod tidy` (Go 1.20.14, офлайн, отдельный модуль) — изменений нет: лишних хэшей в go.sum не было
- checks: `CHK-SQL-04`, `CHK-SQL-05`

### Фаза 2d — подключение без шифрования по умолчанию ✅

- решение заказчика: SQL Server 2008 R2 RTM без SP, основной режим — без шифрования: `encrypt` по умолчанию `disable` (новые профили, пресет `mssql2008`, старый profiles.json без поля); TLS 1.0 и `on`/`optional` — опции (§12 п. 31)
- предупреждение `sql.warn_plaintext_login` (RU/EN) для SQL-аутентификации без шифрования: `sqlcheck.Warnings(profile)`, `Profile.PlaintextLogin()`
- checks: `CHK-SQL-01`, `CHK-SQL-05`

### Фаза 3 — Monitor + alert logic ✅

- `internal/monitor`: цикл опроса всех профилей (период из настроек, 60 с по умолчанию; ≤ 4 параллельно; jitter; изоляция профилей — recover + свой таймаут; остановка по ctx), перечитывание конфигурации по mtime/size и по IPC, `FileCounterStore` (counters-state.json, атомарная запись, восстановление после рестарта, повреждённый файл → сброс + журнал)
- `internal/model`: снимок `StatusSnapshot`/`ProfileSnapshot`/`PointStatus` (FreshAt, лаг, baseline), `AlertEvent`, типы IPC, пороги на профиль (`warnMinutes`/`errorMinutes`), `NewProfile` с пресетом `mssql2008`, ключи сообщений `monitor.*` (RU/EN)
- `internal/alert`: переходы уровней с антидребезгом, тексты RU/EN, Windows Event Log (advapi32), журнал с ротацией
- `internal/ipc` (серверная часть фазы 4): named pipe (overlapped, DACL, first instance), протокол `ping`/`get_status`/`reload_config`/`poll_now` + push статуса и алертов, минимальный клиент
- `internal/winsvc` + `cmd/exchsvc` (часть фазы 5): SCM-обёртка на syscall, служба запускает монитор и IPC-сервер, корректно останавливается по Stop/Shutdown; консольный режим
- интеграционный тест в linux-job CI (SQL Server, compat 100): цикл counters и рестарт монитора
- checks: `CHK-MON-01` … `CHK-MON-09`

### Фаза 3b — устойчивость монитора, IPC и службы ✅

- битый/нечитаемый `profiles.json` или `secrets.dpapi` не применяется: работает последняя успешная конфигурация, counters и алерты сохраняются, предупреждение с антиспамом, повтор на следующей проверке (`CHK-MON-10`)
- IPC: лимит запроса 64 КБ (ответы больше), таймаут чтения и idle-таймаут, ≤ 8 экземпляров канала, `Client.Call(ctx, …)` (`CHK-MON-11`); ограничение частоты `poll_now`/`reload_config` — раз в 10 с, иначе «accepted, coalesced» (`CHK-MON-12`)
- зависший ридер (> 3 таймаутов) закрывается и пересоздаётся, статус `monitor.hung` (`CHK-MON-13`); STOP_PENDING с растущим checkpoint (`CHK-MON-14`); обрезка сообщения Event Log по границе UTF-8 (`CHK-MON-15`)
- уточнения: комментарий `Profile.Encrypt` (`""` = `disable`, п. 31), «снимок показывает error базы сразу» (п. 33), единственный источник пресета `mssql2008` (п. 36)
- checks: `CHK-MON-10` … `CHK-MON-15`
- фаза 3c: `settings.json` не читается/не разбирается → последние успешные настройки + `configStale` (при первом запуске — умолчания, опрос не блокируется), ключ `monitor.config_invalid` для первого запуска (`CHK-MON-10`); брошенный ридер не пишет counters, блокировка профиля в `FileCounterStore` (`CHK-MON-13`); предел роста checkpoint 60 с (`CHK-MON-14`); backoff и антиспам ошибок `Accept` (`CHK-MON-11`)
- фаза 3d: запись counters отклоняется для снятой с учёта entry (смена сигнатуры, удаление профиля), удалённый профиль не воскресает (`CHK-MON-13`); `config_invalid`, пока настройки ни разу не были корректны (`CHK-MON-10`); умолчания `ipc.Server` без `NewServer`, число пропусков в строке восстановления (`CHK-MON-11`); тесты `_AcceptBackoff`, `_StopCheckpoint*`, `_CounterStoreProfileLock` без sleep

### Фаза 4 — IPC

- `internal/ipc` named pipe, протокол — **серверная часть сделана в фазе 3 (CHK-MON-06)**; остаток: клиент трея с переподключением/heartbeat, `TestProfile`, `AckAlert`
- checks: `CHK-IPC-01`, `CHK-IPC-02`

### Фаза 5 — Служба

- `cmd/exchsvc` + `internal/winsvc`, цикл + IPC server — **сделано в фазе 3 (CHK-MON-07)**; остаток: тест под настоящим SCM (регистрация тестовой службы в CI), регистрация источника Event Log с `EventMessageFile` (инсталлятор)
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
11. **`Encrypt ""` = умолчание драйвера** (TLS только для логина, без проверки сертификата) — максимальная совместимость с SQL 2005/2008; для старых серверов без TLS 1.2 — `tlsMinVersion "1.0"` или `encrypt "disable"`. **Заменено п. 31 (фаза 2d):** `""` = `disable`; прежнее поведение — явный `encrypt "optional"`.
12. ~~**Discovery не зависит от `_Node*`** (решение архитектора): `_Node*` даёт только +10; универсальный путь по `_Reference*`/`_InfoRg*`/`_Document*`/`_Node*` и не-1С таблицам; `DiscoverByValue` — поиск по имени известной точки (+ примерное время).~~ — **заменено п. 22–25** (фаза 2b, уточнение заказчика); универсальный путь сохранён.
13. **Только чтение:** все запросы Discovery и опроса — `WITH (NOLOCK)` (с фазы 2c и на каталожных представлениях, без `SET TRANSACTION ISOLATION LEVEL` — §12 п. 29), `TOP (@p1)`, параметры, экранированные имена; бюджеты времени и лимиты таблиц/строк (§5.3). Грязное чтение для мониторинга допустимо; блокировать 1С нельзя.
14. **Linked-опрос — `INNER JOIN`:** точки справочника без строки в таблице времён (например, «этот узел» плана обмена) не показываются; опция `keepUnlinkedPoints` → `LEFT JOIN` (такие точки = Error `no_timestamps`).
15. **Пустая дата 1С:** год < 1900 после вычета `YearOffset` (`2001-01-01` при сдвиге 2000, `1753-01-01` без сдвига) → NULL. `YearOffset` только 0 или 2000.
16. **Типы для IPC в `internal/model`:** `DiscoveryCandidate`, `SampleRow`, `ValueQuery`, `CounterState`/`CounterPoint`/`CounterTimes`, интерфейс `CounterStateStore` — трей не импортирует драйвер. `sqlcheck.DiscoveryCandidate` — alias.
17. **Ошибка соединения → `ProfileStatus.Error`** (уровень Error, фаза 1): отдельного уровня ConnError нет; тип ошибки — `sqlcheck.KindOf(err)` (conn/timeout/canceled/query/mapping/config).
18. **ManualSQL** выполняется в транзакции `READ UNCOMMITTED`, которая всегда откатывается; допускается ровно один `SELECT`/`WITH`, ровно 3 колонки; при заданных и таблице, и ManualSQL побеждает ManualSQL. Фаза 2c: откат — не граница безопасности (п. 28, 30).
19. **Режим `counters`** (решение архитектора через Вику): fallback для конфигураций без хранимых времён; время = момент, когда служба увидела изменение `_SentNo`/`_ReceivedNo`; первое наблюдение после старта — baseline (`sendBaseline`/`recvBaseline`, UI: «не раньше запуска службы»); **уменьшение/сброс счётчика считается изменением**; одна таблица `_Node*` на mapping; в кандидате Discovery колонки счётчиков передаются в `SendCol`/`RecvCol` (→ `sentNoColumn`/`receivedNoColumn`), score фиксированно низкий (5). Хранилище состояния: интерфейс `model.CounterStateStore`, в фазе 2 — только `MemCounterStore`; файл и цикл опроса — фаза 3 (`internal/monitor`).
20. **Интеграционные тесты SQL** — в linux-job CI (контейнер `mcr.microsoft.com/mssql/server:2019-latest`, env `EC_SQL_TEST_HOST/PORT/USER/PASSWORD`); без env тесты пропускаются (windows-job).
21. **Целевая версия MSSQL — SQL Server 2008 (уровень совместимости 100)** (требование заказчика через Вику, фаза 2b): возможно 2008 R2, но ничего из R2 не используем. Все запросы — T-SQL 2005/2008 (§5.2); CHK-SQL-05 проверяет вывод всех построителей, Discovery, DiscoverByValue и шаблонов на список запрещённых конструкций 2012+ (границы слов, без учёта регистра, литералы/комментарии/`[имена]` исключаются; `sys.dm_*` запрещены целиком — DMV не нужны и требуют VIEW SERVER STATE); ручной SQL с ними отклоняется. Интеграционные базы на SQL 2019 создаются с `ALTER DATABASE … SET COMPATIBILITY_LEVEL = 100` (проверяется через `sys.databases` и отказом `STRING_SPLIT`). Переписывать ничего не пришлось — все запросы уже были совместимы с 2005/2008.
22. **Планы обмена `_NodeNNN` — основной источник** (уточнение заказчика, заменяет п. 12): `nodeBonus` = +40 (Discovery и DiscoverByValue); универсальный путь без `_Node` сохранён для самописных конфигураций.
23. **Несколько планов в профиле — `Profile.Mappings []Mapping`** (дополнительные к основному `Mapping`). Обратимо: старые профили не меняются, откат = удалить поле; переиспользует всю машинерию одного mapping (linked/composite/counters/manual, CheckMapping, Preview). Альтернатива «список таблиц `_Node*` внутри одного mapping» отклонена: пришлось бы дублировать linked/composite/counters-поля на каждую таблицу и усложнять SQL ListPoints. `planTable`/`planNo` выводятся из `Table` (не хранятся), `planLabel` хранится. Ключи счётчиков дополнительных mapping — с префиксом `Table + "/"`; счётчики всех mapping — одно состояние профиля.
24. **Составная ссылка 1С 8.1:** фильтр `_TYPE = 0x08 AND _RTRef = N` передаётся параметрами (`@p2`, `@p3`), N = номер таблицы-владельца (`_Node124` → 124) — строки других таблиц с тем же `_RRRef` исключаются в Discovery, ListPoints и поиске по значению.
25. **`DiscoverByValue`: поиск по времени — основной при заданном `ApproxTime`**, поиск по имени — запасной; точку можно задать `pointRef` (`_IDRRef` из `ListNodePoints`) или именем/кодом (+ `planTable`).
26. **Пресет `serverPreset "mssql2008"`** — `TLSMinVersion "1.0"` (Go 1.20: `MinVersion = tls.VersionTLS10` явно), `strict` запрещён; ошибки TLS-рукопожатия — ключи `sql.tls_enable_tls10` / `sql.tls_legacy_failed` / `sql.tls_certificate` с текстами RU/EN в `sqlcheck` (`Message`), пока нет общего каталога (трей, фаза 6); `ProfileStatus.ErrorKey` (`omitempty`). **Уточнено п. 31 (фаза 2d):** пресет по умолчанию даёт `encrypt "disable"`; TLS 1.0 и режимы `optional`/`on` остаются опциями профиля, `strict` с пресетом по-прежнему запрещён.
27. **`encrypt "disable"` = ни TLS для данных, ни TLS для логина** (проверено на проводе): пароль SQL уходит фактически открытым текстом (обратимая маскировка TDS). Рекомендуемый режим для 2008 — пресет (TLS только для логина, TLS 1.0); `disable` — только если сервер не может договориться о TLS вовсе и заказчик принимает риск (§13). **Уточнено п. 31 (фаза 2d):** заказчик принял этот риск — `disable` стал основным режимом; при SQL-аутентификации GUI показывает предупреждение `sql.warn_plaintext_login`.

28. **ManualSQL — ровно один read-only SELECT** (фаза 2c, решение архитектора): в T-SQL операторы не требуют `;`, поэтому `SELECT 1 COMMIT DELETE FROM t` — три оператора, и `COMMIT` завершал бы «всегда откатываемую» транзакцию (проверено на SQL 2019: строка удалялась). Вне строковых литералов, комментариев, `[имён]` и `"имён"` (токенизатор `blankSQL` из CHK-SQL-05, слова целиком, без учёта регистра; `@переменные`/`#temp` — не ключевые слова) запрещены: INSERT, UPDATE, DELETE, MERGE, INTO, EXEC, EXECUTE, COMMIT, ROLLBACK, SAVE, BEGIN, DROP, ALTER, CREATE, TRUNCATE, GRANT, REVOKE, DENY, DBCC, BACKUP, RESTORE, SHUTDOWN, KILL, OPENROWSET, OPENQUERY, OPENDATASOURCE, BULK, WAITFOR, DECLARE, SET, USE, а также TRAN, TRANSACTION, RECONFIGURE, CHECKPOINT, REVERT, SETUSER, DEALLOCATE, OPEN, CLOSE, FETCH, IF, WHILE, GOTO, RETURN, BREAK, CONTINUE, PRINT, RAISERROR, READTEXT, WRITETEXT, UPDATETEXT (`ErrManualForbidden`, в тексте ошибки — само слово). Все они — зарезервированные слова T-SQL, так что колонку с таким именем и так нужно брать в скобки (`[Update]`). На уровне скобок 0 допускается ровно один SELECT (кроме присоединённых UNION [ALL]/EXCEPT/INTERSECT; тела CTE и подзапросы — в скобках), иначе `ErrManualMulti`. WITH/CTE оставлен (шаблон `1c_latest_row`). Проверка — защита от ошибки/небрежности, а не от злоумышленника с правом менять профиль; настоящая граница — права логина (п. 30).
29. **Уровень изоляции не меняется на уровне сессии** (фаза 2c): `SET TRANSACTION ISOLATION LEVEL READ UNCOMMITTED` в каталожных батчах заменён на `WITH (NOLOCK)` на каждом представлении (`INFORMATION_SCHEMA`, `sys.partitions/objects/schemas`, `sysindexes/sysobjects`; работает на compat 100). Причина: уровень, заданный в сессии (SET или begin-transaction TDS), сохраняется после запроса/отката, а `sp_reset_connection` при возврате соединения в пул сбрасывает его **только с SQL Server 2014** — на целевом 2008 соединение пула оставалось бы `READ UNCOMMITTED` для следующих запросов. Ручной SQL по-прежнему идёт в откатываемой транзакции `READ UNCOMMITTED` (TDS begin-transaction, без SET): он выполняется на выделенном `sql.Conn`, после отката выполняется `SET TRANSACTION ISOLATION LEVEL READ COMMITTED` (единственный SET в продукте, `restoreIsolationSQL`); если восстановление не удалось — соединение выбрасывается из пула (`driver.ErrBadConn`). Тест проверяет уровень той же сессии через `sys.dm_exec_sessions` — только в тестах (DMV в продуктовом SQL запрещены CHK-SQL-05).
30. **Логин мониторинга — только `db_datareader`** в каждой контролируемой базе 1С (фаза 2c): без `db_owner`/`db_datawriter`/`sysadmin`, без членства в серверных ролях; `VIEW SERVER STATE` не нужен. Это настоящая граница безопасности для ручного SQL и любых ошибок кода; рекомендация для внедрения — в DELIVERY.md. Windows-аутентификация: учётная запись службы (или указанный `DOMAIN\user`) с тем же правом.

31. **Основной режим подключения — без шифрования** (фаза 2d, решение заказчика: сервер — SQL Server 2008 R2 RTM без SP). `model.DefaultEncrypt = EncryptDisable`: новые профили (`Profile.ApplySQLDefaults()`), пресет `mssql2008` (`ApplyServerPreset`) и профили без поля `encrypt` (старый profiles.json читается как `disable`, `EffectiveEncrypt()`; сырое значение остаётся `""`, файл не переписывается) подключаются с `encrypt=disable`. Явно заданное значение (`optional`/`on`/`disable`) никогда не меняется. TLS 1.0 (`tlsMinVersion "1.0"`) и `on`/`optional` — опции профиля; `strict` с пресетом запрещён. **Предупреждение для GUI (фаза 6):** ключ `sql.warn_plaintext_login` (RU/EN в `sqlcheck.Message`, та же конвенция, что у ключей TLS): показывается при `EffectiveEncrypt() == disable` **и** SQL-аутентификации — пароль идёт почти открытым текстом (обратимая маскировка TDS); рекомендация — Windows-аутентификация или логин только с `db_datareader` (п. 30). Для Windows-аутентификации (SSPI/NTLM/Kerberos пароль не передают) не показывается. Запрос из GUI/трея: `sqlcheck.Warnings(profile) []string` (ключи), `Profile.PlaintextLogin()`; не блокирует сохранение и опрос. В `ProfileStatus` поле не добавлено (файл `internal/model/point.go` — вне зоны фазы 2d, параллельная фаза 3): при необходимости `Warnings []string` добавит фаза 3/6. Подтверждено интеграционно: сессия на базе compat 100 с `encrypt_option = FALSE`.

32. **Статус и уровни (фаза 3, CHK-MON-01):** снимок пересчитывается монитором с `now` службы в UTC+3; `FreshAt = max(LastSend, LastRecv)`, лаг в целых минутах (`-1` — неизвестен, обе даты NULL → Error `no_timestamps`); пороги — `Profile.WarnMinutes/ErrorMinutes` (0 = глобальные из settings.json, по умолчанию 30/60; одиночный override, дающий warning ≥ error, игнорируется); уровень базы — худший из точек; любая ошибка опроса → Error с `errorKey` (TLS-ключи `sql.*` из sqlcheck, иначе `monitor.conn_error`/`timeout`/`query_error`/`mapping`/`config`/`panic`/`busy`/`poll_error`; тексты RU/EN — `model.MonitorMessage`); `baseline` точки — если более свежая сторона в режиме counters ещё «с момента наблюдения».
33. **Антидребезг алертов (фаза 3, CHK-MON-05):** рост уровня точки — сразу (лаг между обменами только растёт), снижение — 2 опроса подряд, ошибка базы — 2 опроса подряд (разовый сбой сети не алертит; `ProfileRaisePolls = 2` сохранён в фазе 3b); **снимок (и трей) показывает error базы сразу**, с первого неудачного опроса — дребезг гасится только для событий алертов и Event Log; пока база в ошибке, уровни её точек заморожены; удалённые/выключенные профили и точки забываются без событий.
34. **Event Log без helper-процессов:** `RegisterEventSourceW`/`ReportEventW` с одной строкой-вставкой; источник `ExchangeChecker` в реестре сервисом не регистрируется (зона инсталлятора, фаза 7) — без `EventMessageFile` просмотрщик показывает текст после преамбулы «описание не найдено»; ID событий ≤ 1000.
35. **IPC-сервер и служба сделаны в фазе 3** (зона бота EC Service): сервер `internal/ipc` + SCM-обёртка `internal/winsvc` на `syscall` (golang.org/x/sys не добавлялся — в go.mod его нет); трею — `ipc.Client`/`DialPipe` и типы в `model`. Pipe-DACL: SYSTEM/Administrators — полный, интерактивные пользователи — чтение/запись без `FILE_CREATE_PIPE_INSTANCE`.
36. **Новые базы — пресет `mssql2008`** (`model.NewProfile`, `DefaultServerPreset`): целевой сервер заказчика — SQL Server 2008 R2 RTM без SP и без TLS 1.2; вместе с п. 31 (фаза 2d) новый профиль получает `encrypt "disable"` и `tlsMinVersion "1.0"` (на случай, если шифрование включат); существующие профили не меняются; запросы по-прежнему проверяются CHK-SQL-05. Предупреждение `sql.warn_plaintext_login` (`sqlcheck.Warnings`) монитор кладёт в `ProfileSnapshot.warnings` — трей показывает его без импорта драйвера. Единственный источник значений — `internal/model`: `DefaultServerPreset` + `ApplySQLDefaults()`/`ApplyServerPreset()` через `NewProfile`; в мониторе копий нет (фаза 3b, `TestCHK_MON_08_SinglePresetSource`).

37. **Сбой загрузки конфигурации (фаза 3b, CHK-MON-10):** если `profiles.json` или `secrets.dpapi` не читается/не разбирается (I/O, битый JSON, версия, DPAPI), пустая конфигурация не применяется — служба продолжает с последней успешной (ридеры, `counters-state.json`, активные алерты не трогаются; удаление состояний/алертов — только после успешной загрузки), снимок: `configError` + `configErrorKey = monitor.config_stale` + `configStale`; предупреждение в журнал и Event Log (warning, ID 110) — одно на новую ошибку, повтор не чаще 15 мин; повтор загрузки на каждой проверке (5 с). Отсутствующий файл по-прежнему = пустой список (не ошибка); невалидный отдельный профиль пропускается (CHK-MON-04). **`settings.json` (фаза 3c)** — так же: нет файла → умолчания; не читается/не разбирается → последние успешные настройки, `configStale`, то же предупреждение с антиспамом, повтор на следующей проверке; профили при этом применяются. Битый `settings.json` при первом запуске — умолчания + предупреждение, опрос не блокируется (в отличие от битого `profiles.json`). Ключ снимка: `monitor.config_stale` (последняя корректная конфигурация) или `monitor.config_invalid` (корректной ещё не было — умолчания / профили не опрашиваются; свои тексты RU/EN). «Корректная» = и настройки, и профили хотя бы раз загрузились успешно (учитываются раздельно, фаза 3d). Невалидные значения (например, `pollIntervalSec` вне диапазона) — умолчания, как раньше.
38. **Лимиты IPC (фаза 3b, CHK-MON-11/12):** запрос ≤ 64 КБ, ответ ≤ 16 МиБ; таймаут чтения начатого кадра 10 с, idle-таймаут 5 мин (трей шлёт `ping` раз в 60 с — `model.IPCHeartbeat`, иначе будет переподключаться); ≤ 8 экземпляров канала / клиентов; `ipc.Client.Call(ctx, cmd)`; постоянная ошибка `Accept` — backoff 200 мс → 30 с, журнал с антиспамом (фаза 3c); строка восстановления — с числом незалогированных повторов, нулевые параметры = умолчания (фаза 3d). `poll_now` (опционально `profileId`) и `reload_config` — не чаще раза в 10 с глобально и на профиль; лишние вызовы: `ok` + `result: "accepted, coalesced"`, опрос не запускается.
39. **Зависший ридер (фаза 3b, CHK-MON-13):** профиль занят дольше 3 таймаутов опроса → ридер (пул `sql.DB`) закрывается в фоне и создаётся заново на следующем опросе, статус error `monitor.hung`, предупреждение в Event Log; брошенный ридер, вернувшись, не пишет состояние counters (проверка поколения при записи + блокировка профиля на Load → Apply → Save в `FileCounterStore`, фаза 3c); то же для entry, снятой с учёта при изменении/удалении профиля (проверка `!retired`), удалённый профиль не восстанавливается в `counters-state.json` (`Retain` под блокировками профилей, фаза 3d). **STOP_PENDING** (CHK-MON-14): checkpoint растёт каждые 2 с, пока служба останавливается, но не дольше 60 с — затем перестаёт расти, чтобы SCM увидел зависшую остановку. **Event Log** (CHK-MON-15): текст обрезается до 31000 байт по границе UTF-8-символа.

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
| Ложные кандидаты Discovery (курсы валют, периодические регистры, документы со свежими датами) | Ранжирование по доле обновлённых точек и двум сериям; `_Node*` +40 (2b); фильтр составных ссылок по RTRef; hints/Reason в GUI; подтверждение пользователем; `DiscoverByValue` (по времени); ручной mapping |
| Строка «этот узел» плана обмена (`_PredefinedID` ≠ 0) | Исключается только тем, что у неё нет строк в регистре времён (`INNER JOIN`); с `keepUnlinkedPoints` она видна как `no_timestamps` |
| SQL 2008 и TLS 1.0 / шифры | Пресет `mssql2008` (`tlsMinVersion "1.0"`), ключ ошибки `sql.tls_enable_tls10`. Go 1.20 по умолчанию предлагает AES-128/256-CBC-SHA (RSA и ECDHE-RSA), AES-GCM и **3DES** (`TLS_RSA_WITH_3DES_EDE_CBC_SHA`, `TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA` — в списке по умолчанию, в конце по приоритету); **RC4 не предлагается** (выключен по умолчанию), CBC-SHA256 тоже. Windows Server 2008/2008 R2 (Schannel) умеет AES-CBC-SHA → договоримся; Windows Server 2003 без KB948963 — только RC4/3DES → договоримся на 3DES (слабый: Sweet32, 64-битный блок; допустимо лишь для защиты логина); сервер только с RC4 → отказ (`sql.tls_legacy_failed`: обновление TLS 1.2 или `encrypt disable`). TLS 1.0 — CBC с неявным IV (BEAST, клиентская атака, для TDS маловероятна). Проверено тестом Go↔Go (`TestCHK_SQL_05_GoTLS10CipherSuites`); на реальном SQL 2008 **не проверено** (в CI — SQL 2019) |
| Ручной SQL: обход проверки | Фаза 2c: запрещённые слова + один SELECT верхнего уровня; токенизатор консервативен (вложенные `/* */` сервер считает комментарием целиком, мы — нет → ложный отказ, не пропуск). Остаток: SELECT по-прежнему может читать любые таблицы в пределах прав логина; фильтр — защита от ошибок, настоящая граница — `db_datareader` (п. 30) |
| `encrypt "disable"` — режим по умолчанию (фаза 2d, решение заказчика) | Нет TLS ни для данных, ни для Login7: пароль SQL фактически открытым текстом (маскировка XOR 0xA5 обратима), запросы/данные читаемы; перехват даёт пароль логина мониторинга. Заказчик принял риск (§12 п. 31). Митигация: Windows-аутентификация (SSPI-токены вместо пароля) или отдельный логин только с `db_datareader` (п. 30), предупреждение `sql.warn_plaintext_login` в GUI; TLS (`optional`/`on`, TLS 1.0) — опция профиля. Если на сервере включено «Force Encryption», `disable` не подключится — выбрать `optional`/`on` |
| Уровень совместимости 100 на SQL 2019 ≠ настоящий SQL 2008 | Большинство функций 2012+ (IIF, TRY_CONVERT, CONCAT…) на движке 2019 работают при любом compat level — настоящая защита — статическая проверка CHK-SQL-05 всех запросов; интеграционные тесты подтверждают, что compat 100 действует (`STRING_SPLIT` отклоняется) и запросы выполняются. Отличия поведения движка 2008 (оптимизатор, версии TDS 7.3) в CI не проверяются |
| Стоимость поиска по времени на больших регистрах | Поиск по ссылке ограничен `MaxLinkedRows`/`MaxLinkTables`/`MatchRows` и бюджетами Discovery; без индекса по ссылке — скан таблицы (NOLOCK, TOP), только по кнопке пользователя |
| Режим counters: время = момент наблюдения, не реальное событие | Флаги baseline; точность = период опроса; состояние в `counters-state.json` (фаза 3) — рестарт службы baseline не сбрасывает; изменения за время простоя службы получают время первого опроса после старта |
| Зависший драйвер / сеть одного профиля (фаза 3) | Свой бюджет времени и горутина на профиль, `recover`; зависший вызов оставляется, профиль помечается `monitor.busy`, остальные опрашиваются; после 3 таймаутов (фаза 3b) ридер закрывается и пересоздаётся (`monitor.hung`) |
| Битый/недописанный `profiles.json` или `secrets.dpapi` (правка вручную, сбой диска, чужой DPAPI-ключ) | Фаза 3b: не применяется, работает последняя успешная конфигурация, counters/алерты сохраняются, предупреждение с антиспамом, автоповтор |
| Локальный клиент канала шлёт огромные кадры / держит соединения / спамит `poll_now` | Фаза 3b: запрос ≤ 64 КБ, таймауты чтения и простоя, ≤ 8 экземпляров, `poll_now`/`reload_config` не чаще 10 с |
| Файл состояния counters повреждён / диск полон | Сброс состояния + запись в журнал + копия `.corrupt`; ошибка записи не останавливает опрос |
| Источник Event Log не зарегистрирован | Запись всё равно идёт (преамбула «описание не найдено»); регистрацию источника — в инсталлятор (фаза 7) |
| Named pipe на NT 6.1 и доступ из сессии пользователя | Overlapped-I/O, DACL с IU (чтение/запись), `PIPE_REJECT_REMOTE_CLIENTS`, first instance против подмены; проверено тестами на windows-latest (не на Win7 — в CI Win7 нет) |

---

## 14. Что не делаем в v1

- Веб-морда, Electron, WPF, tray на сторонних UI-kit
- Уведомления по почте/Telegram
- Кластер из нескольких машин-агентов
- Сборка `.exe`/zip без явной просьбы заказчика
