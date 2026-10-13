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

**Реализовано в фазе 4 (`test_profile`, §6.2, CHK-IPC-03).** Трей → IPC `TestProfile(profileID)` → служба выполняет `ExchangeReader.Test` (или трей может тестировать сам через тот же `internal/sqlcheck` — **решение:** тест делает **служба**, чтобы один стек драйвера/таймаутов; трей только показывает OK/ошибку).

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

**Реализовано в фазе 3 (серверная сторона, CHK-MON-06)** — `internal/ipc`, типы в `internal/model/ipc.go` (трей не импортирует драйвер): кадр = 4 байта длины (little-endian) + JSON; служба → трей ≤ 16 МиБ, трей → служба ≤ 64 КБ (фаза 3b). Трей → служба `IPCRequest{id, cmd, profileId?}`: `ping`, `get_status`, `reload_config`, `poll_now` (последние два — не чаще раза в 10 с, иначе `ok` + `result: "accepted, coalesced"`; таймаут чтения 10 с, idle 5 мин, ≤ 8 клиентов — §12 п. 38). Служба → трей `IPCMessage{id, type, status?, alert?, error?}`: при подключении `hello` (version 1); ответы `pong` / `status` / `ok` / `error` с тем же `id`; push-сообщения (`id` = 0) — `status` после каждого опроса и `alert` на каждое подтверждённое событие. Снимок `StatusSnapshot{seq, now, level, profiles[{profileId, name, enabled, level, points[ExchangePoint + freshAt, lagMinutes, baseline], error, errorKey, freshAt, baseline, warnMinutes, errorMinutes, checkedAt, polled}], alerts (активные), pollIntervalSec, lastPollAt, configError}`, все времена — UTC+3. Pipe: `\\.\pipe\ExchangeChecker`, overlapped-I/O, `FILE_FLAG_FIRST_PIPE_INSTANCE`, `PIPE_REJECT_REMOTE_CLIENTS`, DACL `D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;0x12018b;;;IU)` (интерактивные пользователи — чтение/запись без права создавать экземпляры канала); медленный клиент (очередь 64) отключается, служба не блокируется. `ipc.Client`/`DialPipe` — минимальный клиент для трея. Ещё не реализовано (фаза 6): клиент трея с переподключением и heartbeat-таймером (`CHK-IPC-01`).

**Фаза 4 — протокол версии 2 (`model.IPCVersion = 2`, CHK-IPC-02 … CHK-IPC-05).** Все добавления — новые команды или `omitempty`-поля, поэтому трей версии 1 работает без изменений (неизвестные поля JSON игнорируются, запросы v1 байт-в-байт те же), а новый трей по `hello.version == 1` / отсутствию `capabilities` / ответу `unknown_command` скрывает новые функции.

- `hello`: `{version: 2, capabilities: ["test_profile","ack_alert","heartbeat"], heartbeat}`; `pong`: `{version: 2, heartbeat}`. `Heartbeat{protocol, now, serviceStartedAt, lastCycleAt?, pollerHung, hungAfterSec, pollIntervalSec}` (`pollerHung` всегда явно, в т.ч. `false`, фаза 4b); те же `serviceStartedAt`, `lastCycleAt`, `pollerHung`, `hungAfterSec` — в `StatusSnapshot` (вычисляются в момент чтения). `lastCycleAt` — конец последнего **полного** цикла опроса (одиночный `poll_now` не считается). **Порог зависания** (решение Вики): `hungAfter = max(2 × interval, interval + cycleBudget)`, `cycleBudget = ceil(включённых профилей / MaxConcurrent) × max(таймаут опроса профиля) + max(jitter)`; `pollerHung` = с `lastCycleAt` (до первого цикла — со старта службы) прошло больше `hungAfter`. Сторожевой таймер монитора (своя горутина, раз в 5 с) один раз пишет предупреждение в журнал/Event Log и пушит снимок; восстановление логируется (CHK-IPC-04).
- Ошибки: `error` + `code` (`busy`, `not_found`, `invalid_request`, `no_active_alert`, `unknown_command`, `rate_limited`) + `errorKey` (RU/EN, `monitor.*`); текст `error` остаётся для клиентов v1. `poll_now` для неизвестного профиля теперь тоже `code: not_found` (CHK-IPC-05).
- `test_profile` `{profileId}` (сохранённый профиль, сохранённый пароль) **или** `{profile, password?}` (несохранённый) → `test_result` `{test: {profileId, ok, category, errorKey, error, serverVersion, productLevel, edition, compatLevel, encrypt, tlsMin, durationMs}}`; `category`: `ok` / `network` / `login` / `database` / `tls` / `timeout` / `config` / `other` (значения `sqlcheck.ConnCategory` + жёсткий таймаут службы). Правила (CHK-IPC-03): тест в своей горутине (ping клиента обслуживается параллельно), единый дедлайн 15 с — только через контекст теста, драйвер видит `DeadlineExceeded`, отключение клиента отменяет тест (фаза 4b), не более 2 тестов одновременно и не более 2 «брошенных» (драйвер не вернулся после таймаута) — иначе `busy`; **каждый** запрос `test_profile` сначала проходит общий лимит частоты команд (раз в 10 с, `rate_limited`) — до проверки профиля и пароля (фаза 4b), чтобы канал нельзя было использовать ни как сканер портов, ни как оракул совпадения параметров сохранённого профиля; Windows-аутентификация (служба = учётная запись компьютера) — **только** для сохранённого профиля с сохранёнными параметрами подключения, иначе `category: config` + `monitor.test_windows_unsaved` («Сохраните профиль, чтобы проверить Windows-аутентификацию»), подключения нет; пустой пароль берётся из сохранённого профиля с тем же `id` **только** если совпадают хост, экземпляр (сравнение без учёта регистра только для ASCII), порт (0 = 1433 только без экземпляра), логин и режим аутентификации и шифрование не слабее сохранённого **по каждой оси** (фаза 4b): режим TLS `disable` < `optional` < `on` < `strict`, `tlsMin` не ниже (если сохранённый использует TLS), проверка сертификата не слабее (`on` без `trustServerCert` и `strict` проверяют; `optional` — нет); иначе `invalid_request` + `monitor.test_password_required` («Введите пароль»). Пароль не логируется, не пишется на диск и вычищается из текста ошибок; `IPCRequest.String()` маскирует его. `encrypt=disable` и TLS 1.0 идут одним путём `sqlcheck`.
- `ack_alert` `{profileId, pointKey?}` → `ok` `{result: "acknowledged", acked: N}`; без `pointKey` подтверждаются все активные алерты профиля, `pointKey` = новое поле `PointStatus.key` (= `AlertEvent.key`). Флаги `acknowledged` у точки, у профиля (алерт уровня базы) и у активного алерта (`ackedAt`); уровень остаётся видимым, трей перестаёт повторять уведомление. Сброс (решение Вики): возврат в норму или ухудшение (warning → error) — сброс; улучшение error → warning — подтверждение сохраняется с уровнем warning; пока уровень нового ключа после рестарта ещё не подтверждён антидребезгом — сохраняется. Хранится в `ack-state.json` рядом с `counters-state.json` (атомарная запись, `MoveFileEx`), переживает рестарт; для удалённого профиля — сбрасывается после успешной загрузки конфигурации, `ack_alert` → `not_found`. Event Log алертов не меняется, добавляется одно информационное событие 104 «Тревога подтверждена» (CHK-IPC-02).

**Фаза 6 — служба: запись конфигурации и отмена (G5/G6; CHK-IPC-06, CHK-CFG-03, CHK-IPC-07; контракт с EC Tray, ред. 2, решения Вики 03.10).** Протокол остаётся версии 2: новые команды, `omitempty`-поля и capabilities `config_read`, `config_write`, `cancel` (статический список службы, не зависит от прав клиента).

- **Только служба пишет конфигурацию.** Трей никогда не пишет `settings.json`, `profiles.json`, `secrets.dpapi`; `reload_config` после `save_*` не нужен. Админ-команды: `get_config`, `save_profile`, `delete_profile`, `save_settings` (позже — G2/G3). Проверка на **каждой** админ-команде (без кеша): `ImpersonateNamedPipeClient` → `OpenThreadToken` → `CheckTokenMembership(BUILTIN\Administrators)` → `RevertToSelf`, в отдельной горутине с `LockOSThread` без `Unlock` (поток уничтожается вместе с горутиной — олицетворение не утечёт). UAC-отфильтрованный токен (Administrators — deny-only), клиент с SQOS Anonymous и любая ошибка олицетворения → `access_denied` + `monitor.access_denied_admin`, обработчик не вызывается. Обычный трей показывает настройки только для чтения (по `get_status`), «Изменить (администратор)» запускает свой exe `--settings` через `ShellExecuteExW("runas")` — отдельный процесс, своё соединение.
- **`clientElevated`** (bool) — в ответах на `get_status` и `ping` (не в `hello`: по документации `ImpersonateNamedPipeClient` до первого чтения из канала даёт `ERROR_CANNOT_IMPERSONATE`, а `hello` уходит до первого запроса; windows-тест `TestCHK_IPC_06_ImpersonateBeforeFirstRead` показал, что на windows-latest CI олицетворение до чтения **успешно** — на это не полагаемся, дизайн сохранён: идентичность берётся только после чтения запроса); вычисляется один раз на соединение, только для отображения; в push-снимках его нет.
- **`get_config`** (админ) → `{type: "config", config: {settings, profiles: [Profile + hasPassword]}, revision}` — полные профили, mapping, `manualSql`, пароли никогда.
- **`save_profile`** `{profile, password?, clearPassword?, expectedRevision}` → `ok {result: "saved", profileId, profile (hasPassword), reloaded: true, pollScheduled, revision}`. Пустой `profile.id` → UUIDv4. Пароль (решения Вики): Windows-аутентификация — удалить; непустой — заменить; `clearPassword` — удалить (вместе с паролем — `invalid_request`, `validation.conflict`); пустой — оставить сохранённый **только** если `model.CanReuseSavedPassword(new, saved)` (те же хост/экземпляр/порт/логин/режим и шифрование не слабее по каждой оси), иначе и для нового профиля с SQL-аутентификацией — `invalid_request` + `monitor.test_password_required` («Введите пароль»), ничего не записывается. `hasPassword` от клиента игнорируется (вычисляется по `secrets.dpapi`). Запись атомарная (временный файл + `MoveFileEx`), сначала `secrets.dpapi`, затем `profiles.json` (при ошибке пароль возвращается); затем **синхронное** перечитывание конфигурации — к моменту ответа профиль уже в снимке и `poll_now` для него не даёт `not_found`; опрос включённого профиля ставится в очередь (`pollScheduled`).
- **`delete_profile`** `{profileId, expectedRevision}` → `ok {result: "deleted", profileId, reloaded: true, revision}`; пароль, counters, алерты и подтверждения чистятся; неизвестный → `not_found`.
- **`save_settings`** `{settings, expectedRevision}` (целиком, вкл. `trayAutostart`) → `ok {result: "saved", reloaded: true, revision}`.
- **`trayAutostart` → HKLM Run (CHK-CFG-04, решение Вики 03.10, §12 п. 72):** служба (LocalSystem) применяет настройку к значению `ExchangeCheckerTray` ключа `HKLM\Software\Microsoft\Windows\CurrentVersion\Run` в 64-битном представлении реестра (`KEY_WOW64_64KEY`). Значение — REG_SZ, полный путь `ExchangeCheckerTray.exe` из папки exe службы в кавычках: `"C:\Program Files\ExchangeChecker\ExchangeCheckerTray.exe"`. После успешного `save_settings` (под мьютексом записи конфигурации, после записи и перечитывания): `true` — записать, если значения нет или оно другое; `false` — удалить (значения нет — не ошибка). При старте службы (запуск SCM; консольный режим реестр не трогает) — то же приведение по `settings.json` (нет файла — умолчание `true`; файл не читается — значение не меняется, предупреждение). **Ошибка реестра** (решение EC Service): при старте — предупреждение в журнал службы и Event Log (110), служба работает дальше; в `save_settings` настройки **остаются сохранёнными**, ответ `ok` с новой `revision` (поля предупреждения в контракте `save_settings` нет, контракт не меняем), ошибка — в журнал службы и Event Log (110); следующий `save_settings` или перезапуск службы повторяет приведение. Ручная правка `settings.json` и `reload_config` реестр не меняют (применится при перезапуске). Реестр — через интерфейс `config.RunRegistry` (`config.RegistryRun`: `RegOpenKeyExW`/`RegCreateKeyExW`/`RegQueryValueExW`/`RegSetValueExW`/`RegDeleteValueW`/`RegCloseKey` через `callProc`, `internal/winapi/regview_windows.go`), логика тестируется на linux с фейком.
- **`trayAutostartError` в ответе `get_status` (CHK-CFG-04, фаза 6, capability `tray_autostart_status`):** код ошибки последнего применения `trayAutostart` к HKLM Run (при старте службы или после `save_settings`); успех (или менять нечего) — поля нет (`omitempty`, совместимость CHK-IPC-05). Коды (`model.AutostartErr*`): `access_denied` — реестр отказал в доступе (`ERROR_ACCESS_DENIED`); `registry_error` — другая ошибка реестра; `tray_path` — путь трея не построен из пути exe службы; `settings_unreadable` — при старте `settings.json` не прочитан, значение Run не менялось. Код меняет только следующее применение (успешный `save_settings` или перезапуск службы); ручная правка `settings.json` и `reload_config` его не трогают. `hello.capabilities` содержит `tray_autostart_status` только когда служба управляет значением Run (запуск SCM); в консольном режиме — нет capability и нет поля. Поле только в ответе на `get_status` (не в `status_update` и не в `pong`). Текст для пользователя — словарь трея (служба отдаёт только код). Пример: `{"id":3,"type":"status","status":{…},"trayAutostartError":"access_denied"}`. Tests: `TestCHK_CFG_04_TrayAutostartErrorField` (`cmd/exchsvc`), `TestCHK_CFG_04_AutostartErrorCode` (`internal/config`), windows `_AccessDeniedCode`.
- **G1/G4 в службе (CHK-SQL-04, §12 п. 73):** `profiles[].warnings` в `get_status` — `model.SQLWarnings(profile)` (ключи `sql.*`, тексты RU/EN — `model.SQLMessage`); `errorKey` из ошибки SQL (`sqlcheck.MessageKeyOf`: `sql.tls_*`) в `get_status` и `test_profile` отдаётся, только если ключ известен `model` (`monitor.KnownErrorKey`: `model.MonitorMessage` или `model.SQLMessage`), иначе — ключ категории `monitor.*`. Своих текстов `sql.*` служба не формирует; `validation.sql_*` в `errorArgs` до G3 (`check_mapping` по IPC) не попадают; результат `test_profile` предупреждений не несёт. Tests: `TestCHK_SQL_04_ServiceSQLKeysInModel`, `_ServiceErrorKeys` (`internal/monitor`).
- **`revision`** = hex первых 16 байт SHA-256 от `settings.json` ‖ 0x00 ‖ `profiles.json` ‖ 0x00 ‖ `secrets.dpapi` (сырые байты, отсутствующий файл — пусто), считается с диска под мьютексом записи (запись и перечитывание сериализованы); ручная правка тоже меняет ревизию, после рестарта то же значение; применённая ревизия — `StatusSnapshot.configRevision` — **с фазы 6b считается из тех же байтов, которые разобрал `FileConfig.Load`** (каждый файл читается один раз), поэтому ревизия и применённые данные не расходятся; `save_*` возвращают ревизию своего перечитывания (= `configRevision` снимка). **`expectedRevision` обязателен** для `save_profile`, `delete_profile`, `save_settings` (фаза 6b, решение Вики): без него — `invalid_request` + `monitor.bad_request`, `errorArgs = ["expectedRevision", "validation.required"]` (`model.FieldError`), ничего не записано; клиент берёт ревизию из `get_config` (`revision`) или `get_status` (`configRevision`); несовпадение → `conflict` + `monitor.config_conflict`, ничего не записано.
- **Проверка полей:** `invalid_request` + `monitor.profile_invalid` / `monitor.settings_invalid`, `errorArgs = [путь JSON-поля, ключ причины validation.*, параметры…]` (`model.FieldError`, текст `Error()` прежний; из нескольких ошибок настроек — первая); тексты — `model.FormatMessage(key, errorArgs, lang)` (`{0}` — поле, `{2}…` — параметры). `Profile.Validate` дополнительно отклоняет пустой хост (`\inst`), пустой экземпляр (`host\`) и второй `\` (`validation.server_format`), `manualSql` > 16 КиБ (`validation.too_long`) и больше 64 mapping (`validation.too_many`) — сохранение укладывается в запрос ≤ 64 КиБ.
- **Нечитаемый `profiles.json` / `secrets.dpapi`** (битый JSON, чужой ключ DPAPI) — запись отклоняется (`save_failed` + `monitor.config_save_failed`), чужие данные не теряются; другой невалидный профиль в файле (правка вручную) — `invalid_request` + `monitor.profile_invalid` с его полем; **с фазы 6c ответ называет этот профиль явным полем `profileId`** (`omitempty`, запрос EC Tray) — для `save_profile` и `delete_profile`, только когда ошибка относится к **другому** профилю (ошибка самого сохраняемого профиля — без `profileId`); формат `errorArgs[0]` (путь поля без префикса профиля) и текст `error` не меняются; передаётся через `model.FieldError.ProfileID` (ставит `model.ValidateProfiles`) → `IPCError.ProfileID` → `IPCMessage.profileId`. `save_settings` профили не проверяет, поэтому из-за них не отклоняется. Пример: `{"id":12,"type":"error","error":"profile \"bad\": server: instance is empty after the backslash (want host or host\\instance)","code":"invalid_request","errorKey":"monitor.profile_invalid","errorArgs":["server","validation.server_format","empty_instance"],"profileId":"bad"}`.
- **`secrets.dpapi`** создаётся с DACL `D:P(A;;FA;;;SY)(A;;FA;;;BA)` (SYSTEM + Administrators, без наследования) — дескриптор задаётся при создании временного файла, данные ни мгновения не читаемы с унаследованным DACL. **С фазы 6b существующий файл приводится к SY+BA при старте службы** (`config.Store.SecureSecretsFile` → `SetNamedSecurityInfoW` с `PROTECTED_DACL_SECURITY_INFORMATION`): нет файла — ничего не делается; ошибка — предупреждение в журнал службы, служба продолжает работу (и файл исправится при следующей записи). `profiles.json`, `settings.json` и логи доступны только SYSTEM и Administrators: DACL каталога `D:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)` ставит инсталлятор (§12 п. 90.4).
- **Событие 105** (информационное) «Конфигурация изменена»: действие, `profileId`, учётная запись клиента (`DOMAIN\user`); без паролей и строк подключения (расширение CHK-MON-05).
- **`beep.enabled`** (bool, фаза 6c, §12 п. 65, решение Вики от 03.10): машинный выключатель звука тревог, по умолчанию `true`; `settings.json` без поля (старый) и `null` читаются как `true`, явное `false` сохраняется как `false` (`model.OptBool` — сравнимый тип «не задано / false / true» вместо `*bool`, `Settings` остаётся сравнимой через `==`). Всегда явно отдаётся в `get_status.settings` и `get_config`; принимается `save_settings`; если клиент поле не прислал (не знает о нём), сохраняется текущее значение из файла — звук не включается молча. Пример: `"beep":{"enabled":false,"warningFreqHz":800,"warningDurMs":300,"errorFreqHz":1500,"errorDurMs":500,"gapMs":700}`.
- **Снимок для режима чтения** (`get_status`, без админа): `StatusSnapshot.settings` (пороги, звук вкл/выкл и параметры, язык, `trayAutostart`), `configRevision`; в профиле `connection {server, port, database, authMode, login, hasPassword, encrypt, trustServerCert, tlsMinVersion, serverPreset, connectTimeoutSec, queryTimeoutSec}`, `thresholdOverride {warnMinutes, errorMinutes}` (0 = глобальные), `mappingSummary {mode, configured, manual, plans: [{planTable, planLabel}]}` — **без** `manualSql`, деталей mapping и паролей (новой утечки нет: файлы конфигурации Users не читают — каталог только SY+BA, §12 п. 90.4). **Первая загрузка конфигурации не удалась** (замечание Вики к c163d0f; `profiles.json`/`secrets.dpapi` битые, корректной конфигурации ещё не было): `settings` — разобранный `settings.json` (нет файла — умолчания), а если битый и он — поля `settings` нет (`null`/отсутствует), а не нули (пороги 0, `trayAutostart=false`); причина — `configError` + `configErrorKey = monitor.config_invalid`, `configRevision` пустая; правка `settings.json` до исправления профилей публикуется. Трей поле без `settings` уже обрабатывает (настройки не показываются и не редактируются). CHK-MON-10.
- **G6 — `cancel {ref}`**: только свой выполняющийся асинхронный запрос в своём соединении (`test_profile`; с G2/G3 — `discover`, `discover_by_value`, `list_node_points`, `check_mapping`). Ответ `ok {result: "canceled"}`, сам запрос отвечает `error {id: ref, code: "canceled", errorKey: "monitor.canceled"}`; нет такого (чужое соединение, уже завершён) → `not_found` + `monitor.unknown_request`. Соединение не рвётся; слот теста освобождается, когда драйвер вернётся (лимит брошенных, дальше `busy`). Повтор `id` выполняющегося запроса → `invalid_request`. `ipc.Client.DoCancel` шлёт `cancel` сам при отмене контекста.
- **Каждый ответ `error` несёт `code` и `errorKey`** (поправка 4 EC Tray): `unknown_command` → `monitor.unknown_command`, ошибка без кода → `invalid_request` + `monitor.bad_request`. Новые коды: `access_denied`, `canceled`, `save_failed`, `conflict`.
- Клиент (`DialPipe`) открывает канал с правами `FILE_GENERIC_READ | FILE_WRITE_DATA | FILE_WRITE_ATTRIBUTES` (= 0x12018b из DACL для IU) вместо `GENERIC_WRITE`: `FILE_GENERIC_WRITE` включает `FILE_APPEND_DATA` (= `FILE_CREATE_PIPE_INSTANCE`) и `FILE_WRITE_EA`, которых DACL обычным пользователям не даёт, — не-администратор получал `ERROR_ACCESS_DENIED` (найдено windows-тестом с ограниченным токеном, фаза 6).

**Фаза 6 — служба: G2/G3 — discovery и проверка mapping по IPC (CHK-IPC-08, CHK-IPC-09; контракт §5–§7, дополнение Вики 06.10, §12 п. 74).** Протокол остаётся версии 2: новые команды, `omitempty`-поля и capabilities `discover`, `discover_by_value`, `node_points`, `check_mapping` (объявляются, когда обработчик службы их реализует — в службе всегда; `cancel` тоже).
- **Только администратор** (все четыре команды): проверка клиента (`ImpersonateNamedPipeClient` + `CheckTokenMembership`) на **каждом** запросе, иначе `access_denied` + `monitor.access_denied_admin`, обработчик не вызывается. Порядок проверок: администратор → лимит частоты (`rate_limited` + `monitor.rate_limited`; ключ `discover` общий для `discover`/`discover_by_value`, у `list_node_points` и `check_mapping` свои ключи; 10 с) → профиль/пароль (как `test_profile`: `{profileId}` — сохранённый профиль и пароль; `{profile, password?}` — несохранённый; пустой пароль — сохранённый только при `CanReuseSavedPassword`, иначе `invalid_request` + `monitor.test_password_required`; Windows-аутентификация — только сохранённый профиль с сохранёнными параметрами, иначе `invalid_request` + `monitor.test_windows_unsaved`; неизвестный `profileId` → `not_found`) → поля запроса → лимиты операций.
- **Асинхронно** (как `test_profile`): своя горутина, `ping` того же клиента не ждёт; отмена — `cancel {ref}` (ответ `ok {result:"canceled"}`, сам запрос — `error {code:"canceled", errorKey:"monitor.canceled"}`) и отключение клиента. **Каждый запрос — свой `sqlcheck.NewInteractiveReader` + `Close`** после возврата драйвера: страж читателя закрывает сессию при отмене/истечении бюджета профиля (`KindCanceled`/`KindTimeout`). Служба SQL не составляет — только через `sqlcheck` (read-only, совместимо с SQL Server 2008).
- **Лимиты операций** (`busy`): одна тяжёлая (`discover`, `discover_by_value`) на службу и пул из двух лёгких (`list_node_points`, `check_mapping`) → `monitor.discover_busy`; **одна интерактивная операция на профиль** (любого вида; Вика 06.10) → `monitor.discover_profile_busy`. Ключ профиля: `profileId` сохранённого; у несохранённого — `profile.id`, если задан (окно правит существующий профиль), иначе сервер (`host\instance`, ASCII без учёта регистра) + порт + база. `test_profile` в эти лимиты не входит (свой пул 2). **Жёсткий предел службы 100 с**: ответ не позже (`failed` + `monitor.discover_timeout`), драйвер, не вернувшийся к ответу (отмена/предел), считается брошенным до возврата — не больше 2, дальше `busy` + `monitor.discover_busy` (как у `test_profile`). Горутина операции отправляет результат **один раз и без блокировки** (вызывающий мог уйти по отмене/пределу, когда результат уже в канале, а отложенный `Close` запаниковал — замечание Вики 06.10): счётчик брошенных уменьшается ровно один раз при любом исходе (результат, ошибка, паника в работе или в `Close`), слоты тяжёлой/лёгкой операции и профиля освобождаются на каждом пути; так же у `test_profile` (`monitor.Tester`).
- **Код ошибки `failed` — часть контракта протокола v2** (Вика 06.10; `model.ErrCodeFailed`): ответ `error {id, code:"failed", errorKey, error}` на `discover`, `discover_by_value`, `list_node_points` — запрос принят (админ, частота, профиль, поля, лимиты пройдены), операция выполнялась и не удалась. Возможные `errorKey`: `sql.*` (ключ `sqlcheck`, известный `model`); `monitor.test_network`, `monitor.test_login`, `monitor.test_database`, `monitor.test_tls` (категория подключения), иначе `monitor.conn_error`; `monitor.query_error` (ошибка запроса к базе); `monitor.mapping`; `monitor.discover_timeout` (истёк жёсткий предел службы 100 с или бюджет профиля); `monitor.discover_failed` (прочее). Не `failed`: неверные параметры подключения — `invalid_request` + `monitor.test_config`; отмена — `canceled` + `monitor.canceled`; лимиты — `busy`. `error` — технический текст без пароля. `check_mapping` код `failed` не возвращает: те же ключи приходят в `mappingCheck {ok:false, errorKey, error}`. Трей показывает текст по `errorKey`; неизвестный код трактует как общую ошибку.
- **Код ошибки `internal`** (Вика 06.10; `model.ErrCodeInternal`, `errorKey` — `monitor.internal`, текст RU/EN в `model.MonitorMessage`): паника обработчика асинхронного запроса (`test_profile`, `discover`, `discover_by_value`, `list_node_points`, `check_mapping`) перехватывается в `ipc.Server.startAsync` — служба продолжает работу, запись запроса для `cancel {ref}` удаляется, ресурсы обработчика освобождаются (его `defer`), клиенту — `error {id, code:"internal", errorKey:"monitor.internal"}` с общим текстом **без** текста паники и стека; в журнал службы — команда, `id`, текст паники (пароль и ручной SQL запроса вычищены) и стек (`runtime/debug.Stack`), сам запрос не пишется. Отправка ответа не блокируется (клиент мог отключиться). Трей показывает текст по `errorKey`.
- **Отсутствие поля = пустой результат**: нет `candidates` (`discovery`), `nodePoints` (`node_points`) или `mappingCheck.sampleRows` (`ok:true`) — ничего не найдено / нет строк (`omitempty`, пустой массив не передаётся), а не ошибка и не «нет данных о результате».
- **`truncatedReason`** (`omitempty`, Вика 06.10; `model.TruncatedBudget/Size/Limit`) — рядом с каждым `truncated`: `IPCMessage.truncatedReason` в `discovery` и `node_points`, `mappingCheck.truncatedReason`; есть **тогда и только тогда**, когда `truncated:true`. Значения: `"budget"` — истёк бюджет поиска 90 с (`discovery`); `"size"` — ответ обрезан до 64 КиБ (все три); `"limit"` — `list_node_points` вернул не больше запрошенного `limit` (`node_points`). Несколько причин сразу — старшая: `budget` > `size` > `limit` (бюджет + 64 КиБ → `"budget"`; `limit` + 64 КиБ → `"size"`). Неизвестное значение трей трактует как «результат неполный».
- **`discover`** → `{type:"discovery", candidates:[DiscoveryCandidate], truncated?}`. Бюджет поиска 90 с (`DiscoverOptions.Timeout`): по истечении — частичный результат с `truncated:true`, `truncatedReason:"budget"` (не ошибка; кандидаты несут подсказку `truncated`; без кандидатов — тоже `truncated`, если ушёл весь бюджет). `candidates` без кандидатов отсутствует (`omitempty`).
- **`discover_by_value`** `{…, query: ValueQuery {pointName, pointRef?, planTable?, approxTime?, toleranceMin?}}` → `discovery`. Пустые `pointName` и `pointRef` → `invalid_request` + `monitor.discover_point_name_empty`, `errorArgs = ["query.pointName", "validation.required"]` (ключа `sql.point_name_empty` в `model` нет).
- **`list_node_points`** `{…, limit?}` → `{type:"node_points", nodePoints:[NodePoint {planTable, planNo, ref, code, name}], truncated?}`; `limit` 0 — все прочитанные (`sqlcheck`: до 1000 строк на таблицу плана), 1…10000 — не больше `limit` (прочитано больше — `truncated`, `truncatedReason:"limit"` и `limit: N` — запрошенный `limit`, пожелание EC Tray; поле `IPCMessage.limit` есть только при причине `"limit"`, `omitempty`), иначе `invalid_request` + `monitor.bad_request`, `errorArgs = ["limit", "validation.range", "1", "10000", значение]`; `name`/`code` обрезаются до 256 символов (только показ; точка выбирается по `ref`).
- **`check_mapping`** `{…, mapping: Mapping, limit?: 1..20 (0 = 5)}` → `{type:"mapping_check", mappingCheck:{ok, errorKey?, error?, errorArgs?, violations?, sampleRows?, truncated?, truncatedReason?}}`. Ошибки запроса — протокол (`invalid_request` + `monitor.bad_request`: нет `mapping` → `["mapping","validation.required"]`, `limit` вне 1…20 → `["limit","validation.range","1","20",значение]`). **Ошибка проверки = `ok:false`**: `Mapping.Validate` → задана таблица или ручной SQL → для ручного SQL `sqlcheck.ValidateManualSQL` и **`sqlcheck.Compat2008Violations` до любого обращения к серверу** (`violations` — конструкции SQL Server 2012+) → с подключением `PreviewMapping` (`ValidateMapping` по каталогу + до `limit` строк). `errorKey` полевой ошибки — `monitor.discover_mapping_invalid`, `errorArgs = [mapping.<поле>, validation.* (в т.ч. validation.sql_*), параметры…]` (`model.FieldErrorArgs(err, "mapping.")`; текст — `model.FormatMessage(errorArgs[1], errorArgs, lang)`); ошибки базы — ключ, как у `failed` выше, без `errorArgs`. `sampleRows` ≤ `limit`, имена точек ≤ 256 символов (`pointTruncated`); обрезка до `limit` — не `truncated` (клиент сам просил столько строк).
- **Размер ответа ≤ 64 КиБ JSON** (`model.IPCMaxInteractiveReply`, Вика 06.10) для `discovery`, `node_points`, `mapping_check`: хвост `candidates` / `nodePoints` / `sampleRows` отбрасывается (самый длинный влезающий префикс), ставится `truncated:true` / `mappingCheck.truncated:true` и `truncatedReason:"size"` (кроме `"budget"`, который сохраняется).
- Новые поля: `IPCRequest.query`, `mapping`, `limit`; `IPCMessage.candidates`, `truncated`, `truncatedReason`, `limit`, `nodePoints`, `mappingCheck` — все `omitempty` (CHK-IPC-05); `IPCRequest.String()` не печатает запрос и ручной SQL. Журнал службы: команда, профиль, сервер/база, итог и длительность — без пароля, точек и SQL. Прогресса (`discover_progress`) пока нет.

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
- Тип: own process, LocalSystem; тип запуска — по одному чекбоксу «Запустить службу и запускать автоматически» (по умолчанию включён): включён — AUTO (delayed) и запуск сразу, выключен — DEMAND_START без запуска (§12 п. 90).
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
3. Опции: ярлык трея в меню Пуск; **чекбокс «Запустить службу и запускать автоматически»** (по умолчанию включён; на финише «Готово», §12 п. 90)  
4. Прогресс копирования файлов  
5. Готово  

Действия:

- копировать `ExchangeCheckerSvc.exe`, `ExchangeCheckerTray.exe`, ресурсы;
- создать Uninstall-ключ в Registry + `ExchangeCheckerUninstall.exe` (отдельная маленькая сборка без payload, §12 п. 90);
- зарегистрировать службу;
- опционально стартовать службу (`StartServiceW`);
- **автозапуск трея** при входе пользователя — **обязательно** в v1: ~~HKCU Run~~ HKLM Run, значение `ExchangeCheckerTray` (§12 п. 47, 72): инсталлятор пишет стартовое значение, дальше его ведёт служба по `trayAutostart` (CHK-CFG-04).

Деинсталлятор: остановить/удалить службу, убрать значение `ExchangeCheckerTray` из HKLM Run (64-битное представление), удалить файлы и ключи Uninstall, сохранить или спросить про `%ProgramData%\ExchangeChecker` (по умолчанию настройки оставляем — безопаснее).

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
| `CHK-IPC-02` | `ack_alert`: флаг acknowledged, сброс при норме/ухудшении, сохранение при error → warning, `ack-state.json` после рестарта (фаза 4) |
| `CHK-IPC-03` | `test_profile`: сохранённый/несохранённый профиль, категории, версия/compat, таймаут 15 с, `busy`, `rate_limited`, Windows только сохранённый, правила повторного использования пароля, пароль не в логах/ответах (фаза 4) |
| `CHK-IPC-04` | Heartbeat: старт службы, последний полный цикл, `pollerHung` по порогу (фаза 4) |
| `CHK-IPC-05` | Протокол версии 2: capabilities, коды ошибок, совместимость с клиентами v1 (фаза 4) |
| `CHK-IPC-08` | Discovery по IPC: `discover`, `discover_by_value`, `list_node_points` (фаза 6, G2) |
| `CHK-IPC-09` | `check_mapping` по IPC, включая ручной SQL (фаза 6, G3) |
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

### Фаза 4 — IPC (служба) ✅

- `internal/ipc` named pipe, протокол — **серверная часть сделана в фазе 3 (CHK-MON-06)**; фаза 4 — протокол версии 2 (§6.2): `test_profile` (`CHK-IPC-03`), `ack_alert` с сохранением в `ack-state.json` (`CHK-IPC-02`), heartbeat + `pollerHung` (`CHK-IPC-04`), коды ошибок и совместимость со старыми клиентами (`CHK-IPC-05`)
- тест соединения: `monitor.Tester` с внедряемой функцией (в службе — `sqlcheck.TestConnectionInfo` + `sqlcheck.ConnCategory` от EC SQL; юнит-тесты — фейк и фейковый таймер)
- бэклог 3d: `FileCounterStore.Retain` блокирует все известные ID (данные + таблица блокировок), снятая с учёта entry с вечным опросом закрывает `sql.DB` через `abandonHung`, блокировки counters удалённых профилей удаляются (`CHK-MON-13`); тест `_InvalidWhenSettingsNeverValid` без `sleep` (`os.Chtimes`, `CHK-MON-10`)
- остаток: клиент трея с переподключением/heartbeat-таймером — `CHK-IPC-01` (фаза 6)
- sqlcheck для IPC `test_profile` (по запросу EC Service, §12 п. 40): `TestConnectionInfo(ctx, p, password) (ServerInfo, error)` — версия / уровень продукта / редакция сервера и уровень совместимости базы одним запросом, совместимым с 2008; `ConnCategory(err) string` — категория ошибки подключения (`login`/`database`/`tls`/`timeout`/`network`/`config`/`other`); вся установка сессии ограничена таймаутом подключения (checks `CHK-SQL-01`, `CHK-SQL-05`)
- checks: `CHK-IPC-02` … `CHK-IPC-05`

### Фаза 4b — исправления по ревью Вики ✅

- гонка в продукте (`CHK-MON-13`): `pollOne` возвращал результат до освобождения entry → ложный `monitor.busy` у опроса сразу после цикла (упавший linux CI run 36541312598); entry освобождается (с защитой поколения) до передачи результата
- `test_profile` (`CHK-IPC-03`): единый дедлайн через контекст (без отдельного таймера), отмена при отключении клиента, освобождение слота брошенного теста при возврате горутины; лимит частоты — первым; повторное использование пароля — не слабее по каждой оси шифрования (режим, `tlsMin`, проверка сертификата); хост без учёта регистра только ASCII, порт 0 = 1433 без экземпляра; тест с молчащим TCP-сервером через настоящий `sqlcheck` (закрытие сокета зависит от исправления диалера EC SQL)
- таблицы блокировок профилей со счётчиком ссылок — нет параллельных критических секций при быстром удалении/добавлении профиля (`CHK-MON-13`)
- `pollerHung` без `omitempty` (`CHK-IPC-04`)
- linux CI: `go test -race` для `internal/monitor`, `internal/ipc`, `internal/sqlcheck`, `CGO_ENABLED=1` только в этом шаге (`CHK-CI-01`)

### Фаза 5 — Служба

- `cmd/exchsvc` + `internal/winsvc`, цикл + IPC server — **сделано в фазе 3 (CHK-MON-07)**; остаток: тест под настоящим SCM (регистрация тестовой службы в CI), регистрация источника Event Log с `EventMessageFile` (инсталлятор)
- check: `CHK-SVC-01`

### Фаза 6 — Трей

- `cmd/exchtray` + `internal/wintray`: меню, настройки, статус, модалки, Beep; подфазы 6a–6e (п. 46–56 §12)
- **6a ✅** — клиент `internal/trayipc` (heartbeat, переподключение, рестарт службы, v1/`unknown_command`, проверка PID G7), значок и меню (`internal/trayui` + `internal/wintray`), RU/EN (`internal/i18n`), «О программе», ресурсы exe (`.ico` + манифест + `.syso`), политика сборки трея; «Настройки» в меню неактивен до 6c
- **6b ✅** — тревоги и звук (`CHK-TRAY-03`): окно тревоги поверх всех, бесконечный `Beep` по `settings.beep`, «Закрыть» глушит до следующего изменения, «Подтвердить» → `ack_alert`; логика — `internal/trayalert`
- **6c ✅** — настройки баз (`CHK-TRAY-04/05/06`): «Настройки…» — окно только для чтения по `get_status`, «Изменить (администратор)» → `exchtray --settings` (`ShellExecuteExW` `runas`, своё соединение, G7); форма профиля, «Проверить подключение» (`test_profile`, отмена `cancel {ref}`), `save_profile`/`delete_profile` с `expectedRevision`, «Опросить сейчас»; логика — `internal/traysettings`
- **6d ✅** — Discovery UI (`CHK-TRAY-07`): «Таблицы обмена…» окна администратора (только при capabilities `discover`, `discover_by_value`, `node_points`, `check_mapping`) → диалог: `discover`, `list_node_points`, `discover_by_value`, редактор mapping и шаблоны ручного SQL, `check_mapping` (поле `errorArgs[0]`, `violations`, предпросмотр), «Поиск… N с» + «Отменить» (`cancel {ref}`), «Результат неполный» с причиной `truncatedReason`, «Применить» (с проверкой mapping, слиянием планов без дублей и подтверждением удаляемого) → несохранённая правка профиля → `save_profile` с `expectedRevision`; решения — §12 пп. 81–86; заодно: пауза `Beep` от конца последнего вызова (`CHK-TRAY-03`), `trayAutostartError` рядом с переключателем автозапуска (`CHK-TRAY-09`), тексты `sql.*` через `model.SQLMessage`
- **6e ✅** — общие настройки (`CHK-TRAY-08` финал, `CHK-TRAY-09`): страница «Общие настройки…» окна настроек (интервал опроса, пороги, `beep.*` с выключателем `beep.enabled`, язык журнала службы, `trayAutostart`) → `save_settings` с `expectedRevision` и всегда явным `beep.enabled`; звук выключен — окно тревоги показывается без `Beep`; язык трея — на пользователя (HKCU `Software\ExchangeChecker\Tray`, `Language`), подменю «Язык (Language)», применяется сразу; аудит строк i18n
- checks: `CHK-IPC-01`, `CHK-TRAY-01` … `CHK-TRAY-10`

### Фаза 6 — служба (G5/G6 для трея; EC Service) ✅

- G5: `get_config` / `save_profile` / `delete_profile` / `save_settings` только для повышенного администратора (`access_denied`), `clientElevated`, `revision`/`expectedRevision`/`conflict`, синхронное перечитывание + `pollScheduled`, правила пароля, DACL `secrets.dpapi`, событие 105, расширенный снимок `get_status` для режима чтения, `model.FieldError` + `errorArgs` (§6.2)
- G6: `cancel {ref}`, код `canceled`, реестр асинхронных запросов соединения (сейчас `test_profile`)
- исправление `DialPipe`: права клиента = 0x12018b (иначе не-администратор не подключается)
- checks: `CHK-IPC-06`, `CHK-CFG-03`, `CHK-IPC-07`, расширение `CHK-MON-05`; `FILE_FLAG_FIRST_PIPE_INSTANCE` (G7) — уже `TestCHK_MON_06_NamedPipeFirstInstance`
- **фаза 6b (доработка по Вике, CHK-CFG-03, CHK-IPC-06) ✅:** DACL существующего `secrets.dpapi` — при старте службы (`SetNamedSecurityInfoW`, protected; ошибка — предупреждение); `expectedRevision` обязателен для `save_*`/`delete_profile` (`invalid_request` + `errorArgs ["expectedRevision", "validation.required"]`); `configRevision` снимка — из тех же байтов, что разобрал `Load`
- **фаза 6c ✅ (CHK-CFG-03, CHK-IPC-06):** `beep.enabled` в settings (§12 п. 65); `profileId` в ошибке записи из-за другого невалидного профиля (запрос EC Tray)
- **исправления по windows CI (CHK-MON-02, CHK-IPC-06):** атомарная запись повторяет `MoveFileEx` с коротким backoff (~1,9 с), пока файл держит другой процесс (`ERROR_ACCESS_DENIED`/`SHARING_VIOLATION`/`LOCK_VIOLATION`; `FILE_SHARE_DELETE` у читателя замену не спасает — проверено в CI); конфигурация загружается до того, как канал принимает клиентов; остановка службы не превращает идущую проверку профиля в ответ `invalid_request` (CHK-IPC-03)
- **автозапуск трея в HKLM Run (CHK-CFG-04, решение Вики 03.10, §12 п. 72) ✅:** служба при старте и после `save_settings` приводит значение `ExchangeCheckerTray` в `HKLM\…\Run` (64-бит) к `settings.trayAutostart` (REG_SZ `"<папка службы>\ExchangeCheckerTray.exe"` / удаление); ошибка реестра — предупреждение (журнал + Event Log), служба не падает, сохранённые настройки не откатываются (§6.2); инсталлятор (фаза 7) ставит стартовое значение и удаляет его при деинсталляции
- **G1/G4 (EC SQL, CHK-SQL-02/03/04, §12 п. 73) ✅:** тексты `sql.*` и `validation.sql_*` (RU/EN), `SQLWarnings(profile)`, шаблоны ручного SQL — в `internal/model` (без драйвера); ошибки mapping и ручного SQL — `*model.FieldError` (поле относительно mapping, ключ `validation.sql_*`, параметры) с `errors.Is` к прежним `sqlcheck.Err*`; интерактивное хранилище `sqlcheck.OpenInteractive` / `NewInteractiveReader` (discover, `check_mapping`, sample, предпросмотр ручного SQL, точки плана) — отмена по контексту и бюджет профиля рвут сессию даже у замолчавшего сервера (`KindCanceled`/`KindTimeout`); ячейки выборки ≤ 256 символов (`pointTruncated`), выборка ≤ 100 строк
- **служба: G1/G4 и `trayAutostartError` (CHK-SQL-04, CHK-CFG-04) ✅:** предупреждения `get_status` — `model.SQLWarnings`, `errorKey` SQL — только известные `model`; `get_status.trayAutostartError` + capability `tray_autostart_status` (§6.2)
- **замечания Вики к 49d54ea..c163d0f ✅:** запись `counters-state.json` вне блокировки состояния с номером поколения (CHK-MON-02); снимок без нулевых настроек при неудачной первой загрузке (CHK-MON-10); уборка тестового ключа реестра через `RegDeleteKeyExW` + `KEY_WOW64_64KEY` с проверкой самого ключа (CHK-CFG-04, CHK-TRAY-10); `ERROR_LOCK_VIOLATION` в повторах `MoveFileEx` оставлен
- **G2/G3 в службе (CHK-IPC-08, CHK-IPC-09; §6.2, §12 п. 74) ✅:** `discover`, `discover_by_value`, `list_node_points`, `check_mapping` — только админ, асинхронно с `cancel`, лимиты (1 тяжёлая, пул 2, одна на профиль, брошенные ≤ 2), жёсткий предел 100 с, ответы ≤ 64 КиБ, ручной SQL — `Compat2008Violations` до `PreviewMapping`
- EC Tray 6d (Discovery UI по capabilities) — ✅ (фаза 6 трея); прогресс `discover_progress` — по запросу

### Фаза 7 — Инсталлятор ✅ (7a–7e; решения — §12 п. 90, план — `docs/PHASE7.md`)

- `cmd/exchsetup` + `internal/wininstall`
- HKLM Run `ExchangeCheckerTray`: при установке — стартовое значение (`"<папка>\ExchangeCheckerTray.exe"`, 64-битное представление), при деинсталляции — удалить (§12 п. 72); дальше значение ведёт служба по `trayAutostart` (CHK-CFG-04)
- checks (все implemented, `checks/CHECKS.md`): `CHK-RES-01`, `CHK-VER-01`, `CHK-SETUP-01`…`CHK-SETUP-12`, `CHK-DELIV-01`; payload gzip (stdlib), zip поставки собирается и проверяется в CI без загрузки (7e)
- **EC Tray T1 (подфаза 7c, PLAN-phase7 §7.3/§10.3) ✅ (`CHK-TRAY-11`):** выход трея по `RegisterWindowMessageW(model.TrayExitMessage = "ExchangeChecker.TrayExit")` — главное скрытое окно → `App.Exit()` (значок удаляется, NIM_DELETE), окна `--settings` и Discovery закрываются сразу без вопросов (открытый вопрос отвечает «Нет», после него вопросов нет); `WM_CLOSE` главного окна тоже → `App.Exit()`. Межсеансовое событие (T2) и `.syso` (T3) — не сделаны Контракт доставки: сообщение, отправленное главному окну трея, **никогда** не получает отдельный процесс `--settings`; поэтому инсталлятор шлёт его `HWND_BROADCAST` либо каждому классу — `model.TrayWindowClass` (`ExchangeCheckerTray`), `model.TraySettingsClass` (`ExchangeCheckerTraySettings`; окно только для чтения — `model.TraySettingsViewClass`) и `model.TrayDiscoveryClass` (`ExchangeCheckerTrayDiscovery`). `App.Exit` удаляет значок до ожидания сессии и ждёт её не дольше 2 с (§12 пп. 87–89)
- План и решения: `docs/PHASE7.md`, §12 п. 90. Подфазы (каждая — зелёный CI):
  - **7a ✅** версия (`var Version`, `-X`), ресурсы без внешних тулов (`internal/winres`, `tools/mkres`; `.syso` setup и службы с VERSIONINFO, манифестами, MESSAGETABLE), имена exe в `internal/config`, release-like build в CI — `CHK-RES-01`, `CHK-VER-01`, `CHK-SETUP-11`;
  - **7b ✅** ядро установки/удаления в тихом режиме (payload, SCM, Event Log, ACL, Run, Uninstall-ключ, деинсталлятор) — `CHK-SETUP-01..07`, `CHK-SVC-01`, базовый `CHK-SETUP-12`;
  - 7c обновление поверх — `CHK-SETUP-09`, `CHK-TRAY-11` (EC Tray), `CHK-SETUP-12`;
  - 7d мастер UI, ярлык, RU/EN, запуск трея без повышения — `CHK-SETUP-08`, `CHK-SETUP-10`;
  - 7e zip в CI без загрузки, T3 (трей на mkres), финальный E2E — `CHK-DELIV-01`, `CHK-SETUP-12`.
  - **EC Tray T3 (7e) — на ревью:** `.syso` трея генерирует `tools/mkres` (`internal/winres`): тот же манифест (asInvoker, comctl v6, Win7+, без dpiAware) и значок, плюс VERSIONINFO (`Exchange Checker Tray`, `exchtray`, `config.TrayExeName`); `TestCHK_VER_01_Built` проверяет VERSIONINFO трея, как setup и службы; akavel/rsrc больше не нужен (`CHK-RES-01`, `CHK-VER-01`, `CHK-TRAY-10`).
- **Фаза 8 — UX трея (ветка `phase8-tray-ux`, на ревью):** поля «Тип сервера», свои пороги и тайм-ауты в окне настроек (`CHK-TRAY-12`, умолчания и cue-баннеры — `CHK-TRAY-17`), «Доверять сертификату» только при «Весь сеанс (TLS)» (`CHK-TRAY-13`), тексты ошибок с реальными метками (`CHK-TRAY-14`), подсказки на всех элементах (`CHK-TRAY-15`), меню «Служба» (`CHK-TRAY-16`), `EffectivePort` (`CHK-MODEL-01`), DELIVERY.md про `ДОМЕН\ИМЯ$` (`CHK-DELIV-02`); §12 пп. 91–95.

### Фаза 8 — Поставка

- `DELIVERY.md` + скрипт упаковки **в среде нейронки** (не у заказчика)
- check: `CHK-DELIV-01`
- Сборка zip — **только по явной просьбе**

### Фаза 9 — мастер подключения (EC SQL)

- Ветка `phase9-sql` (в main не вливается до решения Кончиты). Только `internal/sqlcheck`, новые `internal/model/sql*.go`, документы.
- Поиск серверов через SQL Server Browser (SSRP, UDP 1434) — `sqlcheck.Browse` (`CHK-SQL-06`, §12 п. 96)
- Список баз после подключения — `InteractiveReader.Databases` (`CHK-SQL-07`, п. 97)
- Автовыбор сопоставления только при одном уверенном кандидате — `model.SQLAutoPickFor` (`CHK-SQL-08`, п. 98)
- Подсказки по ошибкам подключения RU/EN и генератор скрипта выдачи прав (db_datareader) — `model.SQLDiagnose`, `model.SQLGrantScriptFor` (`CHK-SQL-09`, п. 99); защита от `np:`/`lpc:`/UNC — п. 100
- Структуры запросов/ответов в `internal/model` (независимы от драйвера) для команд службы `sql_browse`, `sql_databases`, `sql_diagnose`, `sql_grant_script` (IPC — EC Service)

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

9. **DPAPI LocalMachine (фаза 1):** `CryptProtectData` с флагами `CRYPTPROTECT_LOCAL_MACHINE` (0x4) и `CRYPTPROTECT_UI_FORBIDDEN` (0x1), опциональный entropy `ExchangeChecker/secrets.dpapi/v1`. Трей (пользователь) шифрует, служба под LocalSystem расшифровывает. Граница доступа — ACL каталога `%ProgramData%\ExchangeChecker\` (инсталлятор, фаза 7: ~~Administrators+SYSTEM+Users-read~~ — **изменено п. 90:** только SYSTEM+Administrators, без Users, логи тоже). (**Изменено решением Вики от 03.10, п. 58:** `secrets.dpapi` — DACL только SY+BA; трей больше не шифрует сам — пишет только служба по IPC.) Tradeoff v1: любой локальный процесс на машине может расшифровать blob — принято как допустимое для v1.

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

28. **ManualSQL — ровно один read-only SELECT** (фаза 2c, решение архитектора): в T-SQL операторы не требуют `;`, поэтому `SELECT 1 COMMIT DELETE FROM t` — три оператора, и `COMMIT` завершал бы «всегда откатываемую» транзакцию (проверено на SQL 2019: строка удалялась). Вне строковых литералов, комментариев, `[имён]` и `"имён"` (токенизатор `blankSQL` из CHK-SQL-05, слова целиком, без учёта регистра; `@переменные`/`#temp` — не ключевые слова) запрещены: INSERT, UPDATE, DELETE, MERGE, INTO, EXEC, EXECUTE, COMMIT, ROLLBACK, SAVE, BEGIN, DROP, ALTER, CREATE, TRUNCATE, GRANT, REVOKE, DENY, DBCC, BACKUP, RESTORE, SHUTDOWN, KILL, OPENROWSET, OPENQUERY, OPENDATASOURCE, BULK, WAITFOR, DECLARE, SET, USE, а также TRAN, TRANSACTION, RECONFIGURE, CHECKPOINT, REVERT, SETUSER, DEALLOCATE, OPEN, CLOSE, FETCH, IF, WHILE, GOTO, RETURN, BREAK, CONTINUE, PRINT, RAISERROR, READTEXT, WRITETEXT, UPDATETEXT (`ErrManualForbidden`, в тексте ошибки — само слово). Все они — зарезервированные слова T-SQL, так что колонку с таким именем и так нужно брать в скобки (`[Update]`). На уровне скобок 0 допускается ровно один SELECT (кроме присоединённых UNION [ALL]/EXCEPT/INTERSECT; тела CTE и подзапросы — в скобках), иначе `ErrManualMulti`. WITH/CTE оставлен (шаблон `1c_latest_row`). Проверка — защита от ошибки/небрежности, а не от злоумышленника с правом менять профиль; настоящая граница — права логина (п. 30). **Уточнение (ревью Вики 06.10, п. 80):** список выше остаётся первым слоем, но больше не единственным: комментарии в ручном SQL запрещены целиком, токенизатор ручного SQL — собственный (`lexManual`), `blankSQL` учитывает вложенность `/* */`; поверх списка — белый список слов SELECT, подсказок таблиц и структуры (п. 80).
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

40. **`test_profile`: сведения о сервере и категория ошибки** (фаза 4, по запросу EC Service; существующий API не менялся):
    - `sqlcheck.TestConnectionInfo(ctx, p, password) (ServerInfo, error)`, `ServerInfo{Version, ProductLevel, Edition string; CompatLevel int}` (JSON `version`/`productLevel`/`edition`/`compatLevel`, `omitempty`). После Ping — один запрос `serverInfoSQL`: `SERVERPROPERTY('ProductVersion'/'ProductLevel'/'Edition')` через `CAST(… AS nvarchar(128))` + `(SELECT compatibility_level FROM sys.databases WHERE database_id = DB_ID())`; T-SQL 2005/2008, один autocommit SELECT без SET (п. 29), в наборе CHK-SQL-05 (`server_info`). NULL → `""`/0. Если запрос отклонён сервером как запрос (нет прав на `sys.databases`) — повтор только с `SERVERPROPERTY` (`serverPropsSQL`, `server_info_props`), `CompatLevel = 0`, ошибки нет; ошибки подключения/таймаута возвращаются. Логин только с `db_datareader` (п. 30) видит уровень своей базы (проверено интеграционно). `TestConnection` — обёртка над `TestConnectionInfo` (дополнительный запрос не меняет результат: ошибки запроса игнорируются). Также `(*SQLStore).ServerInfo(ctx)`.
    - `sqlcheck.ConnCategory(err) string`: `login` (SQL 18456, 18452, 18486–18488), `database` (4060, **4063** — так SQL 2019/2022 сообщают о несуществующей базе при логине, 4064), `tls` (ошибки с TLS-ключом `sql.tls_*`, та же классификация `tlsKey`), `timeout` (дедлайн контекста, `os.ErrDeadlineExceeded`, net-таймаут, `KindTimeout`), `network` (`net.Error`/`OpError`/`DNSError`, отказ/сброс соединения, `driver.ErrBadConn`, SQL 233/10054, SQL Browser / «no instance matching»), `config` (`KindConfig`: профиль не превращается в подключение, неверный DSN), `other` (в т.ч. отмена контекста и ошибки запросов); `nil` → `""`. Проверяются все токены ошибки (`mssql.Error.All`) и обёртки (`errors.As`); часть причин go-mssqldb v1.8.2 отдаёт только текстом — запасное сравнение по тексту. Константы `ConnCategory*`.
    - **Таймаут установки сессии (исправлено после ревью архитектора):** go-mssqldb v1.8.2 ограничивает таймаутом и контекстом только TCP-dial, а ответы prelogin/TLS/login читает без дедлайна и без контекста — сервер, принявший TCP и молчащий, вешал вызывающего, сокет и `sql.DB` навсегда. `Open` использует `guardedConnector` (обёртка `mssql.Connector` + `handshakeDialer`): на каждый `Connect` — страж с машиной состояний под мьютексом `handshaking → done | killed | failed`. Страж реагирует на **любое** завершение контекста вызывающего (`Canceled` и `DeadlineExceeded`) и на бюджет `connectTimeoutSec`, но действует только в `handshaking`: сокетам ставится прошедший дедлайн и они закрываются, вызов возвращается сразу (ошибка содержит причину — `ConnCategory` даёт `timeout`, отмена — `KindCanceled`). Ошибка prelogin/TLS/login → сокеты закрываются сразу (не ждём GC); `TestConnection(Info)` закрывает `sql.DB` на всех путях. Успех фиксируется обёрткой **после** возврата драйвера, под тем же мьютексом: переход в `done` снимает дедлайн сокета (`SetDeadline(time.Time{})`), после этого страж не может действовать — отложенный `cancel()` драйвера и дедлайн, сработавший сразу после логина, рабочую сессию не трогают; длинные запросы опроса не обрываются. Если страж успел убить сессию, пока драйвер возвращал успех, соединение закрывается и возвращается ошибка.
    - **После логина (фаза 4, доработка по ревью):** проверено по исходникам go-mssqldb v1.8.2 и тестом — после логина драйвер на завершение контекста запроса отправляет ATTENTION и затем ждёт подтверждения блокирующим чтением без дедлайна (`tokenProcessor.nextToken` → `readCancelConfirmation`, чтение в горутине `processSingleResponse`); если сервер замолчал после логина, Ping или запрос висят до TCP keepalive (навсегда, если хост отвечает на keepalive). Поэтому **только в `TestConnectionInfo`/`TestConnection`** сессия остаётся под стражем и после логина (`sessionHold`): Ping (бюджет 2 × `connectTimeoutSec`) и запрос сведений (`queryTimeoutSec` + `connectTimeoutSec`) выполняются как операции стража; если контекст операции (или вызывающего) завершился, пока операция идёт, сокеты сессии получают прошедший дедлайн и закрываются, ошибка — `timeout`/`canceled` (`ConnCategory`: `timeout`/`other`). Между операциями и после них страж ничего не делает; сессии, открытые после срабатывания, закрываются сразу. **Хранилища опроса (`Open`) этого стража не имеют** — регулярный опрос не обрывается; от зависшего опроса защищает монитор (CHK-MON-13, пересоздание ридера).
    - **Запасной запрос сведений (исправление по ревью):** если `serverInfoSQL` отклонён (нет доступа к `sys.databases`), а запасной `serverPropsSQL` тоже не выполнился, возвращается ошибка запасного запроса (обрыв сети → `network`, истечение → `timeout`), а не первая ошибка запроса; `TestConnectionInfo` считает проверку успешной (CompatLevel 0) только если запасной запрос выполнился.

41. **Протокол IPC версии 2 (фаза 4, CHK-IPC-05):** только новые команды и `omitempty`-поля; `hello.capabilities`, `code`/`errorKey` в ошибках; клиенты v1 не ломаются (§6.2).
42. **`test_profile` (фаза 4, CHK-IPC-03, решения Вики):** тест делает служба (п. 5.4), через `sqlcheck`; таймаут 15 с, ≤ 2 параллельно, `busy`; под общим лимитом частоты команд (`rate_limited`); Windows-аутентификация — только сохранённый профиль с сохранёнными параметрами (иначе `config` + «Сохраните профиль, чтобы проверить Windows-аутентификацию»); сохранённый пароль — только на тот же хост/экземпляр/порт/логин/режим аутентификации и не через более слабое шифрование (иначе `invalid_request` + «Введите пароль»).
43. **`ack_alert` (фаза 4, CHK-IPC-02, решения Вики):** подтверждение сохраняется при улучшении error → warning (с уровнем warning), сбрасывается при норме и при ухудшении; хранится в `ack-state.json`, переживает рестарт; Event Log алертов не меняется, одно информационное событие «тревога подтверждена» (ID 104).
44. **`pollerHung` (фаза 4, CHK-IPC-04, решение Вики):** порог `max(2 × interval, interval + cycleBudget)`, `cycleBudget = ceil(n / MaxConcurrent) × max таймаут профиля + max jitter` (§6.2).

45. **Фаза 4b (ревью Вики 63d4be6):** таймаут `test_profile` — единственный дедлайн контекста (диалер видит `DeadlineExceeded`); лимит частоты `test_profile` — до любых проверок; сохранённый пароль — только если запрос не слабее по режиму TLS, `tlsMin` и проверке сертификата; хост/экземпляр — ASCII без учёта регистра, порт 0 = 1433 только без экземпляра; `pollerHung` всегда явно; `-race` в linux CI с cgo только для этого шага.

46. **Конфиг пишет только служба (фаза 6, G5):** `save_profile`, `delete_profile`, `save_settings` принимаются только от клиента с повышенным токеном администратора (служба проверяет через `ImpersonateNamedPipeClient`), т. к. по п. 9 у Users только чтение. Обычный трей открывает настройки только для чтения с кнопкой «Изменить (администратор)» — она перезапускает трей с `--settings` через `ShellExecuteW` `runas` (WinAPI, не os/exec). `save_profile` перечитывает конфиг синхронно — `poll_now` сразу после сохранения не даёт `not_found`. Пустой пароль в `save_profile` = оставить сохранённый, сброс — явный флаг `clearPassword`. Сторона службы (уточнено п. 57: только при `CanReuseSavedPassword`).
47. **Автозапуск трея:** HKLM Run, пишет инсталлятор; HKCU не используется. `trayAutostart` — машинная настройка, меняется через `save_settings`; в 6e трей только показывает/переключает её. (**Уточнено п. 72:** значение по `trayAutostart` ведёт служба, инсталлятор ставит стартовое и удаляет при деинсталляции.)
48. **Строка состояния службы** — неактивный пункт меню над списком баз.
49. **Цвет базы** — цветной квадрат через `MENUITEMINFO.hbmpItem` (32-бит DIB), без owner-draw; цвет продублирован текстом статуса (доступность).
50. **Окно тревоги:** закрытие только глушит звук до следующей смены статуса; подтверждение — отдельная кнопка «Подтвердить».
51. **Ресурсы трея:** `cmd/exchtray/rsrc_windows_amd64.syso` хранится в репозитории (для версии `0.0.0-dev`) вместе с исходными `exchtray.ico`, `exchtray.manifest` и заметкой `cmd/exchtray/RESOURCES.md`; с фазы 7e (T3) его, как `.syso` setup и службы, генерирует `tools/mkres` на `internal/winres` (только std, в go.mod ничего не добавлено, п. 90; akavel/rsrc больше не используется): `go run ./tools/mkres`, свежесть — `mkres -check` / `TestCHK_RES_01_CommittedSysoFresh`, в CI release-like build перегенерирует его с версией сборки. Манифест: asInvoker, Common Controls v6, supportedOS Win7+, без dpiAware. Значок — те же ID ресурсов, что у setup (группа 1, значки 1–3). VERSIONINFO: `ProductName` «Exchange Checker», `FileDescription` «Exchange Checker Tray», `InternalName` `exchtray`, `OriginalFilename` `config.TrayExeName`, версии = версия сборки, без CompanyName/LegalCopyright (`CHK-VER-01`).
52. **G7 обязателен:** до первой команды трей сравнивает PID сервера канала (`GetNamedPipeServerProcessId`) с PID службы из SCM (`QueryServiceStatusEx`); при несовпадении пароль не отправляется, показывается ошибка. Служба создаёт канал с `FILE_FLAG_FIRST_PIPE_INSTANCE` (сторона службы).
53. **Общие настройки** — в фазе 6, подфаза 6e. Язык — на пользователя в HKCU-ключе трея, без прав администратора.
54. **Сохранение профиля с Windows-аутентификацией** удаляет сохранённый пароль (сторона службы).
55. **Порядок работ EC Service:** сначала G5 (нужен для 6c), затем G1+G4, затем G2+G3 (для 6d); G6 желательно вместе с G5. Контракты команд согласуются с EC Service и фиксируются в §6.2 до начала кодирования.
56. **Win7 в CI не проверяется** — ручная приёмка у заказчика. `CHK-TRAY-10`: нет API новее NT 6.1, все вызовы через `LazyProc` с `Find()`, список импортируемых функций сверяется с документированными минимальными версиями.
57. **Фаза 6, G5 — запись конфигурации только службой (решения Вики 03.10, CHK-IPC-06/CHK-CFG-03; уточняет п. 46 и 54):** команды `get_config`, `save_profile`, `delete_profile`, `save_settings` — только клиенту с повышенным токеном администратора (`ImpersonateNamedPipeClient` + `CheckTokenMembership(BUILTIN\Administrators)` на **каждой** команде, иначе `access_denied`); трей повышает себя через `ShellExecuteExW "runas"` своего exe `--settings`, обычный трей — режим чтения по `get_status` (`get_config` — только админ: там `manualSql` и полный mapping). Пароль: пусто = оставить только при `CanReuseSavedPassword`, новый SQL-профиль без пароля → «Введите пароль», `clearPassword` — удалить, Windows-аутентификация — удалить; в логах/ответах/событиях — никогда. Синхронное перечитывание после записи; `revision`/`expectedRevision`/`conflict` сделаны сразу; лимиты `manualSql` ≤ 16 КиБ, `mappings` ≤ 64. `clientElevated` — в ответах `get_status`/`ping` (не в `hello`), только для отображения. Протокол остаётся v2 (capabilities).
58. **`secrets.dpapi` — DACL SY+BA без наследования (решение Вики от 03.10, CHK-CFG-03):** DPAPI LocalMachine расшифровывает любой локальный процесс, поэтому граница доступа к паролям — ACL самого файла: `D:P(A;;FA;;;SY)(A;;FA;;;BA)`, задаётся при создании временного файла (до `MoveFileEx`), существующий файл со старым (унаследованным) DACL приводится к SY+BA при старте службы (`SetNamedSecurityInfoW` + `PROTECTED_DACL_SECURITY_INFORMATION`, фаза 6b; ошибка — предупреждение в журнал, служба не падает) и при следующей записи паролей. Остальные файлы каталога (`profiles.json`, `settings.json`, логи) получают от каталога наследуемый DACL SY+BA без Users (§12 п. 90.4); защищённый DACL самого `secrets.dpapi` инсталлятор не меняет. Windows-тест проверяет DACL.
59. **Событие 105 и отмена (фаза 6):** информационное событие 105 «Конфигурация изменена» — учётная запись, действие, `profileId`, без паролей и строк подключения (расширение CHK-MON-05). **Фаза 7:** инсталлятор регистрирует в `EventMessageFile` события 104 и 105 (вместе с 100–103, 110). G6: `cancel {ref}` + код `canceled` (CHK-IPC-07) — только свой запрос в своём соединении, соединение не рвётся.
60. **Тревоги трея — когда звук (6b, CHK-TRAY-03, реализовано):** «изменение статуса» = новая тревога или ухудшение уровня; улучшение error → warning и снятие тревог звук не включают.
61. **Потеря связи со службой при открытой тревоге (6b, реализовано):** окно и звук сохраняются, «Подтвердить» неактивна до восстановления связи.
62. **Подтверждение (6b, реализовано):** тревога уровня базы подтверждается одним `ack_alert` по профилю целиком без `pointKey` (подтверждает и точки этой базы), тревога точки — по `pointKey`.
63. **`not_found` / `no_active_alert` на `ack_alert` (6b, реализовано):** тревога уже снята или подтверждена — скрыть и не показывать снова, пока уровень не изменится.
64. **Пауза звука (6b, исправлено по ревью):** между вызовами `Beep` всегда не меньше 50 мс — при любой `gapMs` меньше 50 (в т. ч. 0) и при сбое динамика (ошибка `Beep`).
65. **Выключатель звука (заменяет решение 2 по 6b):** поле `beep.enabled` в `settings` (машинное, по умолчанию `true`) добавляет EC Service (**реализовано, фаза 6c службы**, CHK-CFG-03/CHK-IPC-06: старый файл без поля = `true`, явное `false` сохраняется, `save_settings` без поля сохраняет текущее значение); трей его соблюдает, переключатель — в 6e; окно тревоги показывается и при выключенном звуке.
66. **Окно настроек в уже повышенном трее (6c, решение архитектора):** при `clientElevated = true` и capability `config_write` трей редактирует настройки в своём процессе без `runas`; служба всё равно проверяет права на каждой команде.
67. **Несохранённые правки (6c):** при правках формы смена профиля, «Добавить», «Перечитать» и закрытие окна спрашивают «Отменить изменения?» (MessageBoxW, по умолчанию «Нет»); без правок — без вопроса.
68. **Удаление базы (6c):** подтверждение MessageBoxW «Удалить базу …?» (по умолчанию «Нет»).
69. **Запуск `--settings` (6c, уточняет п. 46):** `ShellExecuteExW` `runas` с `SEE_MASK_NOCLOSEPROCESS`; отказ UAC — `FALSE` и `GetLastError() = ERROR_CANCELLED`; по дескриптору процесса — `AllowSetForegroundWindow(GetProcessId)` (уже открытое окно настроек выходит вперёд), затем `CloseHandle`.
70. **Шифрование strict (6c):** показывается в списке режимов, с пресетом `mssql2008` отклоняется валидацией.
71. **Тайм-ауты запросов окна настроек (6c):** ожидание ответа `test_profile` — 25 с (служба отвечает за 15 с), прочих запросов — 10 с; тайм-аут Discovery — позже (6d).
72. **Автозапуск трея — HKLM Run ведёт служба (решение Вики от 03.10, CHK-CFG-04; уточняет п. 47):** служба (LocalSystem) применяет `settings.trayAutostart` к значению `ExchangeCheckerTray` в `HKLM\Software\Microsoft\Windows\CurrentVersion\Run` (64-битное представление, `KEY_WOW64_64KEY`): REG_SZ `"<папка exe службы>\ExchangeCheckerTray.exe"` (в кавычках) при `true`, удаление при `false`; при старте службы и после каждого `save_settings`. Ошибка реестра не роняет службу и не откатывает сохранённые настройки — предупреждение в журнал службы и Event Log (§6.2). **Инсталлятор (фаза 7)** ставит стартовое значение `ExchangeCheckerTray` и удаляет его при деинсталляции (DELIVERY.md). HKCU Run не используется.
73. **SQL для IPC службы (G1/G4, EC SQL, CHK-SQL-02/03/04):** (а) тексты: `model.SQLMessages()` (ключ → `{ru, en}`), `model.SQLMessage(key, lang)`, `model.SQLWarnings(profile)` (ключи `sql.*`), `model.SQLManualTemplates()` (`id`, `title`/`description` RU/EN, `sql`); `sqlcheck.Message`/`Warnings`/`ManualSQLTemplates` — обёртки; `model.FormatMessage` понимает `sql.*`/`validation.sql_*` (в `MonitorMessage` их нет — словарь трея). (б) Ошибки `CheckMapping`/`ValidateMapping`/`PreviewMapping`/`ValidateManualSQL`/ручного SQL несут `*model.FieldError`: `Field` — имя поля mapping без префикса (`table`, `lastSendColumn`, `manualSql` …; служба добавляет `mapping.`/`mappings[i].` — `model.FieldErrorArgs(err, prefix)`), `Key` — `validation.sql_*` (или `validation.*` из `Mapping.Validate`), `Args` — имя таблицы/колонки/типы/запрещённое слово; `Error()` — прежний английский текст; `errors.Is` с `ErrManual*`, `ErrTableNotFound`, `ErrColumnType` … сохранён; извлечение — `sqlcheck.FieldErrorOf` / `model.AsFieldError` / `model.FieldErrors`. (в) Разовые интерактивные операции (discover, discover by value, точки плана, `check_mapping`, sample, предпросмотр ручного SQL) — через `sqlcheck.NewInteractiveReader` (или `OpenInteractive`): одна сессия, каждый вызов под стражем — конец контекста вызывающего (`cancel {ref}`) или бюджет профиля (запрос + подключение; Ping — 2 × подключение) во время вызова закрывает сокет, ответ — `*sqlcheck.Error` `KindCanceled` (`ConnCategory` `other`) / `KindTimeout` (`timeout`), горутины не остаются; следующий вызов того же читателя открывает новую сессию. Хранилища опроса (`Open`/`NewReader`) — без стража (п. 40, CHK-MON-13). Пределы: DNS и SQL Browser до TCP не прерываются; срабатывание рвёт всю сессию читателя (один запрос на читатель). (г) Выборка: `PreviewMapping` — не больше `model.MaxSampleRows` = 100 строк; имя точки в `SampleRow` (sample, discover, discover by value) — не длиннее `model.MaxSampleCellRunes` = 256 символов (руны, граница UTF-8), флаг `pointTruncated`; даты не усекаются; худший случай ≈ 100 × 1 КиБ.
74. **G2/G3 в службе (дополнение Вики 06.10, CHK-IPC-08/09):** все четыре команды только для администратора, `cancel {ref}`; кроме контрактных лимитов (одна тяжёлая на службу, пул 2 для `list_node_points`/`check_mapping`) — **одна интерактивная операция на профиль** (`busy` + `monitor.discover_profile_busy`; ключ — `profileId` / `profile.id` / сервер+порт+база); **ответ ≤ 64 КиБ JSON** (хвост отбрасывается, `truncated`); **`truncatedReason`** рядом с `truncated` (`budget` / `size`, оба → `budget`; EC Service добавил `limit` для `list_node_points`, старшинство `budget` > `size` > `limit`); код `failed` — часть контракта, отсутствие `candidates`/`nodePoints`/`sampleRows` — пустой результат; **всё SQL — совместимо с SQL Server 2008**, служба SQL не составляет, ручной SQL — `Compat2008Violations` до `PreviewMapping`. Решения EC Service: ключ частоты `discover` общий для `discover`/`discover_by_value`, у `list_node_points` и `check_mapping` свои (иначе типовой порядок «точки → поиск → проверка» упирался бы в `rate_limited`); новый код `failed` (операция выполнялась и не удалась) — для ошибок подключения/запроса/предела 100 с в `discover*`/`list_node_points`; пустая точка — `monitor.discover_point_name_empty` (ключа `sql.point_name_empty` в `model` нет); `test_profile` в лимиты G2/G3 не входит. Подробности — §6.2.
75. **Общие настройки и профили (6e, утверждено Викой):** страница общих настроек и профили баз — в одном окне настроек и с одной ревизией конфигурации (`get_config.revision`, `expectedRevision` для `save_profile` / `delete_profile` / `save_settings`).
76. **Языки (6e, утверждено Викой):** язык службы (журнал событий) — поле `language` общих настроек (машинное); язык трея — на пользователя, `HKCU\Software\ExchangeChecker\Tray\Language` (`ru` / `en`, нет значения — как в Windows), переключается в подменю ПКМ «Язык (Language)».
77. **Смена языка трея (6e, утверждено Викой):** применяется сразу к меню, подсказке значка и окну тревоги; уже открытые окна (настройки, «О программе») получают новый язык при следующем открытии.
78. **Язык окна `--settings` (6e, утверждено Викой):** экземпляр `--settings`, запущенный через `runas`, читает язык из HKCU учётной записи, под которой выполнено повышение (при вводе учётных данных другого администратора — его HKCU).
79. **Пароль в буферах IPC (принятое ограничение, закрывает п. 5 ревью 6c):** JSON запроса и строка Go в общем `ipc.WriteRequest` после `test_profile` / `save_profile` не обнуляются — копия пароля всё равно остаётся в поле EDIT; трей читает поле пароля только при сохранении/проверке в обнуляемый буфер UTF-16.
80. **Ручной SQL — усиление (ревью Вики 06.10 контура Discovery, CHK-SQL-04/05; уточняет п. 28):** (а) **комментарии запрещены целиком** — `--` и `/*` вне `'…'`/`N'…'`, `[…]`, `"…"` → `validation.sql_comment` (`ErrManualComment`): SQL Server вкладывает `/* */`, и `SELECT 1 AS a,2 AS b,3 AS c /* /* */ ' */ COMMIT DELETE FROM dbo.t --'` проходил прежнюю проверку (комментарий закрывался на первом `*/`, хвост считался строкой), а сервер выполнял COMMIT и DELETE (подтверждено интеграционным контролем); незакрытые строка/`[имя]`/`"имя"` → `validation.sql_manual_literal`; вне литералов допускаются только ASCII и буквы/цифры/диакритика Unicode (неразрывный пробел, U+200B, управляющие → `validation.sql_manual_forbidden` с `U+XXXX`); `blankSQL` (CHK-SQL-05) тоже учитывает вложенность. (б) **Подсказки таблиц — только `NOLOCK`, `READUNCOMMITTED`, `NOWAIT`** (`WITH (…)` и старая форма `t (NOLOCK)`); любые другие (`TABLOCKX`, `HOLDLOCK`, `UPDLOCK`, `XLOCK`, `PAGLOCK`, `ROWLOCK`, `SERIALIZABLE`, `REPEATABLEREAD`, `READCOMMITTEDLOCK`, `READPAST`, `INDEX(…)`, `FORCESEEK`, `NOEXPAND`, `SNAPSHOT` …, также без `WITH`) и подсказки соединения `LOOP`/`HASH`/`REMOTE JOIN` → `validation.sql_manual_hint` ({2} — подсказка): права SELECT достаточно, а блокировка таблицы на рабочей 1С остановит пользователей; `OPTION (…)` → `validation.sql_manual_option`. `WITH имя AS (` в начале — CTE, `TOP … WITH TIES` и `GROUP BY … WITH ROLLUP|CUBE` разрешены, прочий `WITH` после начала — второй оператор. (в) **Белый список вместо одного чёрного:** запрос начинается с SELECT или WITH (CTE), ровно один SELECT на уровне скобок 0 (цепочки UNION/EXCEPT/INTERSECT); любое зарезервированное слово T-SQL вне набора слов SELECT (ALL, AND, ANY, AS, ASC, BETWEEN, BY, CASE, COALESCE, COLLATE, CONTAINS(TABLE), CONVERT, CROSS, CURRENT_TIMESTAMP, CURRENT_USER, DESC, DISTINCT, DOUBLE, ELSE, END, ESCAPE, EXCEPT, EXISTS, FOR, FREETEXT(TABLE), FROM, FULL, GROUP, HAVING, IN, INNER, INTERSECT, IS, JOIN, LEFT, LIKE, NOT, NULL, NULLIF, ON, OR, ORDER, OUTER, OVER, PERCENT, PIVOT, PRECISION, RIGHT, SELECT, SESSION_USER, SOME, SYSTEM_USER, TABLESAMPLE, THEN, TOP, UNION, UNPIVOT, USER, VALUES, VARYING, WHEN, WHERE, WITH) → `validation.sql_manual_forbidden`; незарезервированные глаголы операторов (RECEIVE, SEND, MOVE, GET, ENABLE, DISABLE, THROW, CONVERSATION, SIGNATURE, DIALOG) на уровне скобок 0 запрещены, кроме псевдонима после `AS` и части имени через точку (колонку с таким именем — `[Send]`); `END` только закрывает `CASE` (`END CONVERSATION` отклоняется); чёрный список п. 28 — первый слой. (г) **Лимит строк на сервере:** на выделенном соединении `ManualPoints` перед запросом — `SET ROWCOUNT <limit>` (целое число, не текст пользователя), после отката — одним пакетом `SET TRANSACTION ISOLATION LEVEL READ COMMITTED; SET ROWCOUNT 0`; ошибка восстановления — соединение выбрасывается (как в фазе 2c); других сессионных SET в запросах службы нет (`TestCHK_SQL_04_NoSessionSetInProductSQL`). `SET ROWCOUNT` для SELECT действует на SQL Server 2005–2022 (CHK-SQL-05). Граница безопасности по-прежнему — права логина (п. 30); проверка не разбирает T-SQL полностью и может отклонить редкий корректный запрос (например, колонку `Send` без скобок) — сообщение называет слово.
81. **Discovery в трее — отдельный модальный диалог (фаза 6d, CHK-TRAY-07):** кнопка «Таблицы обмена…» окна настроек — только в окне администратора (повышенный процесс) и только при всех четырёх capabilities `discover`, `discover_by_value`, `node_points`, `check_mapping`; открывает отдельное окно-диалог (владелец неактивен, пока диалог открыт; повторное «Настройки» / второй запуск `--settings` выводит вперёд диалог). Диалог помещается в рабочую область экрана (`SPI_GETWORKAREA` + `AdjustWindowRectEx`, Windows 7 при 1024×768 с панелью задач); ошибки и результаты проверки — полным текстом в многострочном поле только для чтения (в т. ч. длинная подсказка `sql.tls_*` про TLS 1.0).
82. **Логика Discovery — `internal/traysettings/discovery.go` (фаза 6d):** окно `internal/wintray/discoverydlg_windows.go` только показывает состояние и пересылает команды; запросы, тексты, проверка и слияние mapping тестируются без окна. Запросы `test_profile` и все четыре команды Discovery собираются по правилам `test_profile` (сохранённый профиль без правок соединения — `profileId`, иначе форма + пароль) **без mapping**: неверная правка mapping не блокирует ни проверку подключения, ни поиск. Ошибки `mapping.*` / `mappings[i].*` называются по-русски/по-английски («Таблицы обмена — Ручной SQL», «Доп. план обмена 2 — …») и фокусируют кнопку «Таблицы обмена…»; `query.*` и `limit` — свои подписи и поле/кнопку диалога.
83. **Лимиты Discovery в трее (фаза 6d):** `list_node_points` запрашивает `limit` 1000 (`truncatedReason` `limit` — точек больше); клиентский тайм-аут команд 110 с (`traysettings.DiscoveryTimeout`, служба отвечает не дольше 100 с); ответ, пришедший после закрытия диалога или после более нового запроса, игнорируется.
84. **«Применить» — несохранённая правка профиля (фаза 6d, уточнено ревью 06.10):** перед применением mapping проверяется (`model.Mapping.Validate` и правила `Profile.Validate` для `mappings`); ошибка остаётся в диалоге с подписью и фокусом на поле (поле, которого нет в текущем режиме, — на поле, которое его вызывает: ручной SQL / таблица точек). Смена профиля в окне настроек за время диалога — отказ; пока окно настроек ждёт `get_config`/сохранения — отказ с просьбой повторить. Правка сохраняется обычным `save_profile` с `expectedRevision`; ответ `get_config` после переподключения не стирает правки, сделанные пока он шёл (в т. ч. применённый mapping): тогда только сравнивается ревизия. «Все планы обмена» **сливает** найденные планы с уже имеющимися `mappings` профиля без дублей: план = схема (пусто = `dbo`, без учёта регистра) + таблица плана (иначе таблица точек), свои `mappings` профиля сохраняются, найденный план добавляется, только если его ещё нет; если при применении что-то уходит из профиля (прежний основной mapping другого плана, доп. mapping того же плана, что выбранный основной), диалог перечисляет это и спрашивает подтверждение (по умолчанию «Нет»). Закрытие диалога (крестик, «Закрыть», Esc) с непримененными изменениями спрашивает, как окно настроек (п. 67); новый поиск, другой кандидат или шаблон поверх правок, сделанных вручную, спрашивают; ответ поиска, пришедший пока пользователь правил поля, правки не затирает.
85. **Ручной режим mapping (фаза 6d, уточнено ревью 06.10):** редактор работает в одном из трёх видов — «Таблицы», «Ручной SQL», «Счётчики»; mapping собирается **только из видимых полей текущего вида** (переключение вида очищает поля, которые новый вид не использует; скрытые нередактируемые поля не проверяются и не отправляются в `check_mapping`). Скрытые поля связи кандидата (`timesSchema`, `timesLinkTypeColumn`/`timesLinkRTRefColumn`/`timesLinkRTRef`) сохраняются, только пока схема, таблица точек, таблица времён и колонка связи совпадают с кандидатом; изменённая вручную таблица их сбрасывает. Метка источника (`Mapping.mode`, profile.go): `auto` — только пока таблицы и колонки подтверждённого кандидата не изменены; колонки, изменённые вручную, и ручной SQL — `manual`; счётчики — `counters`; диалог показывает метку («Источник: …»). **Вид определяется так же, как опрашивает служба** (ревью 06.10 a5b13b1): непустой `manualSql` — всегда «Ручной SQL», даже если задана и таблица (`sqlcheck` `usesManualSQL` / `ReaderFor` → `QueryReader`); такой mapping открывается со своим SQL, а переключение в «Таблицы» возвращает его табличную часть. **Инвариант:** «Применить» без правок в диалоге не создаёт правку профиля (`SetMapping` не вызывается) и никогда не меняет то, что опрашивает служба (SQL ↔ таблицы), и скрытые поля mapping; правка — только изменённая форма. Вопрос «заменить ручные правки?» при выборе кандидата недействителен, если за это время ответ поиска заменил список (счётчик списков); при подтверждении удаления в «Применить» заново проверяются занятость окна, ревизия и mapping профиля — если `get_config` успел их обновить, ничего не применяется и диалог просит нажать «Применить» ещё раз.
86. **Тексты `sql.*` в трее (фаза 6d):** `i18n.Message` переводит ключи `sql.*` и `validation.sql_*` через `model.SQLMessage` (один источник текстов для службы и трея); все `model.SQLMessageKeys()` локализованы RU/EN (тест перебирает весь список); подсказка допуска поиска по точке: «0 или пусто — 1 сутки (1440 мин)» (`model.DefaultValueToleranceMin`).

87. **Выход трея по `ExchangeChecker.TrayExit` — кто что закрывает (EC Tray T1, CHK-TRAY-11):** окна настроек и Discovery по сообщению только закрываются (Discovery — вместе со своим окном настроек); выходит из трея только главное окно `ExchangeCheckerTray`. Отдельный процесс `--settings` сообщение главному окну не получает, поэтому инсталлятор шлёт его через `HWND_BROADCAST` (или каждому классу из `model.Tray*Class`).
88. **«Да» во время выхода считается «Нет» (EC Tray T1):** если сообщение выхода пришло, пока открыт вопрос (MessageBox / `ask`), ответ игнорируется — после выхода ничего не сохраняется, не удаляется и не применяется, новых вопросов нет.
89. **`ChangeWindowMessageFilter` не нужен (EC Tray T1):** UIPI пропускает сообщения от процесса с более высоким уровнем целостности (повышенный инсталлятор) к процессу с равным или более низким (трей, повышенный `--settings`).

90. **Фаза 7 — инсталлятор, решения координатора (12.10.2026; план — `docs/PHASE7.md` §D):**
    1. Один чекбокс «Запустить службу и запускать автоматически», по умолчанию включён; выключен — `SERVICE_DEMAND_START` без запуска (POLICY §2, §8, §9).
    2. Включён — Automatic (Delayed Start). 3. Failure actions: перезапуск через 60 / 120 / 300 с, сброс 86400 с, `FAILURE_ACTIONS_FLAG = TRUE`.
    4. `%ProgramData%\ExchangeChecker` — `D:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)`, **без Users** (логи тоже; заменяет «Users-read» п. 9 и 58). Папка установки — `D:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;FRFX;;;BU)`, защищённый DACL всегда (п. 90.17).
    5. Publisher / CompanyName / LegalCopyright пустые: в VERSIONINFO не пишутся, `Publisher` в Uninstall-ключе нет. Значок setup — `cmd/exchtray/exchtray.ico`.
    6. Версия: релиз — тег `vX.Y.Z`, CI — `0.0.0-ci.<run_number>`, `internal/version.Version` — `var`, задаётся `-ldflags -X` (CHK-VER-01); даунгрейд — отказ (код 3), только `/ALLOWDOWNGRADE`; pre-release/dev — переустановка с предупреждением.
    7. Канал поставки: CHK-CI-01 не меняется (артефакты не загружаются); zip собирается и проверяется внутри job, заказчику — по явной просьбе; пересмотр на фазе 8.
    8. После установки/обновления трей запускается без повышения в своём сеансе (токен `explorer.exe` + `CreateProcessWithTokenW`) — запуск процесса продукта, не helper-процесс (POLICY §4); не вышло — «значок появится при следующем входе», не ошибка; в тихом режиме не запускается.
    9. Трей в чужих сеансах — `TerminateProcess` (межсеансовое событие не делаем); `TerminateProcess` зависшей службы — только с согласия, в тихом режиме — ошибка. **Исключение (решение архитектора, 13.10.2026):** при откате обновления инсталлятор может без согласия (в том числе в `/S`) завершить **свою только что установленную V2-службу** (`ExchangeCheckerSvc.exe` из папки установки, образ проверяется по тому же дескриптору процесса) — иначе откат невозможен; в журнал пишется WARNING. Для старой службы, работавшей до обновления, правило согласия не меняется.
    10. `internal/winres` + `tools/mkres` — EC Setup; `.syso` `cmd/exchsetup` и `cmd/exchsvc` (MESSAGETABLE с ID событий службы) генерирует и коммитит EC Setup; `config.ServiceExeName` / `SetupExeName` / `UninstallExeName` — EC Setup. EC Service: `alert.EventIDs()` (S1), `TestCHK_SVC_02_MessageTable` (S3), контракт ReportEventW (S4). EC Tray: `model.TrayExitMessage` и выход по нему (T1, 7c), трей на mkres (T3, 7e).
    11. Деинсталлятор — отдельная маленькая сборка без payload `ExchangeCheckerUninstall.exe`. 12. Самоудаление — файл до перезагрузки + код 3010 допустимо.
    13. Гейтированные E2E (`EC_SETUP_E2E`, `EC_SVC_E2E`) могут трогать настоящий HKLM только на одноразовом windows-раннере с очисткой (CHECKS.md, рядом с CHK-CFG-04).
    14–15. Только amd64, служба только под LocalSystem. 16. Чужой владелец каталога ProgramData — рекурсивно владелец BA + DACL, предупреждение в журнал, защищённый DACL `secrets.dpapi` не трогается.
    17. **Защищённый DACL на папке установки — всегда**, в любой выбранной папке (в том числе вне Program Files), владелец — BA.
    18. Без подписи кода в v1 (DELIVERY.md). 19. Сжатие payload — решение после 7b. 20. Язык описания службы и ярлыка — язык установки, ярлык только на трей. 21. Без `dpiAware`. 23. Ключ источника Event Log при удалении удаляется. 22, 24, 25 — как в `docs/PHASE7.md`.

91. **Пороги, тайм-ауты и тип сервера — в окне настроек (фаза 8, `CHK-TRAY-12`, `CHK-TRAY-17`):** поля показывают действующее значение (своё или унаследованное из общих настроек / умолчание кода); значение, равное унаследованному или умолчанию, сохраняется как 0 — профиль продолжает следовать общим настройкам. Поэтому «свой» порог, совпадающий с общим, не фиксируется (осознанно). Пара «Ошибка» > «Предупреждение» проверяется по действующим значениям.
92. **«Доверять сертификату сервера» — только при `encrypt=on` (фаза 8, `CHK-TRAY-13`):** в `optional` драйвер сертификат не проверяет, в `strict` проверяет всегда (суть TDS 8.0); флажок там отключён и в профиль не пишется. Код подключения службы не менялся.
93. **Тексты ошибок называют элементы окна (фаза 8, `CHK-TRAY-14`):** `sql.tls_*` ссылаются на реальные флажки/пункты, ошибки проверки показывают метки полей и пунктов списков, а не JSON-имена.
94. **`Profile.EffectivePort` исправлена, не удалена (фаза 8, `CHK-MODEL-01`):** 0 для `хост\экземпляр` без порта (SQL Browser), как в `sqlcheck.driverConfig`; вызовов в продукте нет.
95. **Меню «Служба» (фаза 8, `CHK-TRAY-16`):** состояние, запуск и остановка через SCM с минимальными правами на каждый шаг; без повышения прав (при `ERROR_ACCESS_DENIED` — сообщение), конфигурация службы не меняется. Новых WinAPI нет (все уже в списке CHK-TRAY-10).
96. **Поиск SQL Server (фаза 9, `CHK-SQL-06`):** SSRP по UDP 1434: широковещательный `CLNT_BCAST_EX` (0x02) на 255.255.255.255 и направленные broadcast каждого IPv4-интерфейса, для хоста — `CLNT_UCAST_EX` (0x03) или `CLNT_UCAST_INST` (0x04) для экземпляра. Ответы собираются до тайм-аута (по умолчанию 2 с, 0,2–15 с; для хоста — пока не ответят все его адреса), ctx отменяет. Разбор `SVR_RESP` (0x05, длина LE): обрезанные/битые датаграммы отбрасываются, значения — валидный UTF-8 без управляющих символов, ≤ 255 байт; порт 1..65535 или 0; дедупликация по ServerName + InstanceName; не более 256 экземпляров и 48 КиБ JSON (`Truncated`). Сеть — `net.ListenPacket` прямо в `internal/sqlcheck` **по явной задаче фазы 9** (UDP-broadcast в `hcwinnet` нет; согласование с POLICY — за Кончитой). Нет ответа ≠ ошибка (Browser остановлен, брандмауэр).
97. **Список баз (фаза 9, `CHK-SQL-07`):** `sys.databases`, только `database_id > 4`, `state = 0` (ONLINE) и `HAS_DBACCESS = 1`; имя, уровень совместимости, состояние. Только чтение, без SET сессии, через интерактивную защиту (guarded); запрос — в наборе CHK-SQL-05. Лимит настраиваемый (по умолчанию 256, максимум 1000) плюс 48 КиБ JSON, флаг `Truncated`; в ответе время подключения и запроса отдельно (`connectMs`/`queryMs`).
98. **Автовыбор сопоставления (фаза 9, `CHK-SQL-08`):** применяется без вопроса только если лучший кандидат один и уверенный: нет нескольких планов обмена, не счётчики, кандидат полный (есть отправка и приём, точки, нет подсказок truncated/no_timestamps/future_dates/single_series), оценка ≥ 120 и отрыв от второго ≥ 30 (калибровка на фикстурах: узел 1С 148/102 — применяется; два плана 155/148, произвольный регистр 117/97, справочник с датами 100/97, общий 95/51 — нет). Иначе — `apply=false` и ключ причины `sql.autopick_*`.
99. **Подсказки и скрипт прав (фаза 9, `CHK-SQL-09`):** «что делать» RU/EN по категориям ошибки подключения (login/database/tls/timeout/network/config/other) и по ключам `sql.tls_*`. Скрипт для администратора: SQL-логин, `ДОМЕН\ИМЯ` или `ДОМЕН\КОМПЬЮТЕР$` (NT AUTHORITY\SYSTEM на том же компьютере), роль `db_datareader` в одной базе; совместим с 2005/2008, повторный запуск безопасен (`IF NOT EXISTS` по `sys.server_principals`/`sys.database_principals`), имена только в литералах `N'…'` и через `QUOTENAME` на сервере, проверка имён (управляющие символы, длина ≤ 128, формат Windows). Пароль SQL-логина в скрипт не подставляется: заглушка `{{PASSWORD}}`, без замены скрипт останавливается с ошибкой; `CHECK_POLICY = ON`, `CHECK_EXPIRATION = OFF`.
100. **Запрет `np:`/`lpc:`/`admin:`/UNC в имени сервера (фаза 9) — ожидает окончательного решения Кончиты:** пользовательская проверка (`FieldError`, `validation.sql_server_forbidden`) — в `internal/model/validation.go`, делает EC Service в ветке `phase9-svc`. В `sqlcheck` — только внутренняя защита перед подключением (`forbiddenServerForm`, неэкспортируемая): префиксы `np:`, `lpc:`, `admin:`, `tcp:`, `sm:`, `via:`, ведущие `\`/`/`, путь `\pipe\`. go-mssqldb v1.8.2 с URL-DSN из `driverConfig` такие префиксы не разбирает (именованные каналы и shared memory не подключены: пакеты `namedpipe`/`sharedmemory` не импортируются) — имя уходит в TCP как есть; защита от этого не зависит. **TODO:** после влития `phase9-svc` в main — вызвать `model.ValidateSQLServerAddr` (ключ `validation.sql_server_forbidden`, реализует EC Service) в `sqlcheck` перед подключением (сейчас в `phase9-sql` его нет).
101. **Запросы EC Tray к мастеру (фаза 9, `CHK-SQL-06`…`09`):** совместимо, без ломки: `sqlcheck.ListServers` (= `Browse`), запрос `limit`, ответ `partial` (обрезка / битые ответы `malformed` / unicast-хост ответил не всеми адресами; broadcast всегда идёт до тайм-аута — это не partial). `sqlcheck.ListDatabases(ctx, profile, password, opt)` для несохранённого профиля; у базы `is1c` (две из таблиц Config/Params/DBSchema), `hasNodeTables` (`_Node*`), `probed`, `sizeMb` (`sys.master_files`, при отсутствии прав 0 без ошибки); проба — трёхчастные имена `[база].sys.tables`, без USE/SET, без NOLOCK (на представления каталога подсказка не нужна), не более 64 баз, ошибки прав пропускаются. `DiscoveryCandidate` **не меняется** (файл `discovery.go` вне зоны фазы 9): ранг, уверенность 0..1 (= score/200) и причины `sql.cand_*` — в `SQLAutoPick.ranks` (по индексу кандидата), порог `autoPickMin` = 0,6, флаг `autoApply`. Скрипт прав: `auth "sql"|"windows"` (вместе с `windows`), `tables` — `GRANT SELECT` только на эти таблицы (`таблица` = `dbo.таблица` или `схема.таблица`, ≤ 100, проверка и `QUOTENAME`); программа скрипт только показывает. Ошибки: `ErrorKind`/`ConnCategory` без изменений, добавлен `sqlcheck.ErrorInfo(err) → model.SQLErrorInfo{code, category, errorKey, tlsKey, sqlNumber}` с кодами `login_failed`, `database_unavailable`, `tls`, `timeout`, `network`, `browser_unreachable`, `instance_not_found`, `permission_denied` (229/297/300/916), `not_1c_database`, `config`, `other`; все ключи `sql.diag_*`/`sql.cand_*` — в `model.SQLMessage` (`KnownErrorKey`).

### Ещё желательно (не блокер каркаса)

- Доступ к одной тестовой MSSQL для калибровки Discovery.


---

## 13. Риски

| Риск | Митигация |
|------|-----------|
| Медленный логин (SSPI/Kerberos, перегруженный сервер 2008) | С фазы 4 вся установка сессии (TCP + prelogin + TLS + login) ограничена `connectTimeoutSec` (по умолчанию 15 с, §12 п. 40); при таймауте на медленном сервере — увеличить `connectTimeoutSec` в профиле (до 600) |
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
| Канал как сканер портов / перебор паролей через `test_profile` (фаза 4) | Общий лимит частоты команд (раз в 10 с, `rate_limited`), ≤ 2 теста, таймаут 15 с; сохранённый пароль — только тому же серверу/логину и не через более слабое шифрование; Windows-аутентификация (учётная запись компьютера, риск NTLM relay) — только для сохранённого профиля |
| Драйвер не отпускает тест после таймаута (фаза 4) | Ответ `timeout` всё равно через 15 с; брошенных тестов ≤ 2, дальше `busy` до их завершения. Фаза 4b: единый дедлайн контекста и отмена при отключении клиента — диалер `sqlcheck` (исправление EC SQL) закрывает сокет, слот освобождается |
| Параллельные критические секции counters при быстром удалении/добавлении профиля (фаза 4b) | Блокировки профилей со счётчиком ссылок: не удаляются, пока их держат или ждут |
| Оракул параметров сохранённого профиля через `test_profile` (фаза 4b) | Лимит частоты до любых проверок; сравнение по каждой оси шифрования; ASCII-сравнение хоста (без Unicode-свёртки) |
| Ложный `pollerHung` при многих недоступных базах (фаза 4) | Порог учитывает бюджет цикла: `max(2 × interval, interval + cycleBudget)` |
| `ack-state.json` повреждён (фаза 4) | Копия `.corrupt`, подтверждения сброшены (тревоги снова не подтверждены), служба работает |
| Обход проверки администратора для записи конфигурации (фаза 6) | Проверка на каждой команде, без кеша; олицетворение в отдельном OS-потоке, который уничтожается (`LockOSThread` без `Unlock`); любая ошибка (анонимный клиент, олицетворение до чтения) = отказ; windows-тесты: повышенный админ — разрешено, токен с BA deny-only и SQOS Anonymous — `access_denied` |
| Антивирус / индексатор / резервное копирование держит файл конфигурации или состояния открытым | `MoveFileEx` повторяется с backoff ~1,9 с; при более долгой блокировке запись настроек — `save_failed`, запись counters/подтверждений — предупреждение в журнал (мониторинг не прерывается) |
| Два администратора правят настройки одновременно / ручная правка файла (фаза 6) | `revision` с диска под мьютексом записи, `expectedRevision` обязателен (фаза 6b; без него — `invalid_request`), несовпадение → `conflict`, ничего не записано |
| Строже `Profile.Validate` (фаза 6: `\inst`, `host\`, второй `\`, `manualSql` > 16 КиБ, > 64 mapping) | Такой профиль из файла, правленного вручную, пропускается при загрузке (как любой невалидный), сохранение других профилей — `invalid_request` с его полем, пока его не исправят или не удалят; через трей такие значения не сохраняются |
| Пароли в `secrets.dpapi` читаемы любым локальным процессом (DPAPI LocalMachine) | Фаза 6: DACL файла SY+BA без наследования (п. 58), пишет только служба |
| Named pipe на NT 6.1 и доступ из сессии пользователя | Overlapped-I/O, DACL с IU (чтение/запись), `PIPE_REJECT_REMOTE_CLIENTS`, first instance против подмены; проверено тестами на windows-latest (не на Win7 — в CI Win7 нет) |

---

## 14. Что не делаем в v1

- Веб-морда, Electron, WPF, tray на сторонних UI-kit
- Уведомления по почте/Telegram
- Кластер из нескольких машин-агентов
- Сборка `.exe`/zip без явной просьбы заказчика
