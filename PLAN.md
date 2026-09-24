# PLAN.md — детальный план Exchange Checker

Статус: **на согласование** (код фич не начинать до ОКка заказчика по открытым вопросам в §12).

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
| `secrets.dpapi` | пароли профилей, защищённые `CryptProtectData` (машина/пользователь — уточнить в §12) | трей |
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
  AuthMode      string   // "sql" | "windows" (если согласуем Windows auth)
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

«Простой» считаем от **макс(время_отправки, время_приёма)** по точке или отдельно по направлению — см. §12 (нужно решение заказчика). В плане по умолчанию: **отдельно** для Send и Receive; алерт, если **хотя бы одно** направление отстаёт.

### 4.4. Точка обмена (результат опроса)

```text
ExchangePointStatus {
  ProfileID     string
  PointName     string   // имя узла / точки из БД
  LastSendUTC   time     // nullable
  LastRecvUTC   time     // nullable
  SendLagMin    int
  RecvLagMin    int
  Level         OK | Warning | Error | Unknown | ConnError
  Message       string
  CheckedAt     time
}
```

---

## 5. MSSQL: что читаем

### 5.1. Задача

В каждой указанной базе 1С 8.1 (файловый вариант не целевой — только SQL) найти по **точкам обмена** две метки:

- дата/время последней **отправки** сообщений;
- дата/время последнего **приёма** сообщений.

### 5.2. Стратегия доступа

1. Драйвер: чистый TDS-клиент на Go **без CGO** (кандидат: `github.com/denisenkom/go-mssqldb` или аналог, совместимый с Go 1.20; финальный выбор — на этапе spike check `CHK-SQL-01`).
2. Подключение: host/port/db + SQL auth (обязательно); Windows auth — опционально после согласования.
3. Запрос времён — **через настраиваемый SQL-шаблон** в профиле или общий шаблон в `settings`, чтобы не привязаться навсегда к одной версии метаданных 1С.

### 5.3. Рабочая гипотеза схемы 1С 8.1 (требует подтверждения)

В типовых РИБ 1С 8.x сведения об обмене часто живут в таблицах плана обмена / регистрации изменений. Точные имена зависят от конфигурации.

**Фаза spike (до основной разработки монитора):**

- check `CHK-SQL-01`: TestConnection + выполнение списка таблиц/вьюх с именами, похожими на обмен (`%Node%`, `%Exchange%`, `%Обмен%` в комментариях — осторожно с кодировками).
- check `CHK-SQL-02`: на **одной реальной базе заказчика** (или дампе схемы) зафиксировать канонический `SELECT`, возвращающий колонки:

```sql
-- целевая форма результата (имена колонок фиксируем контрактом)
PointName NVARCHAR(...),
LastSend  DATETIME,   -- или DATETIME2
LastRecv  DATETIME
```

До подтверждения схемы код `sqlcheck` развивается за интерфейсом:

```go
type ExchangeReader interface {
    Test(ctx) error
    ListPoints(ctx) ([]ExchangePointStatus, error)
}
```

Реализации: `StubReader` (тесты), `QueryReader` (шаблон SQL), позже `V81DefaultReader` после калибровки.

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

- ЛКМ по иконке → popup/окно списка: профили → точки обмена → LastSend / LastRecv / уровень (цвет).
- ПКМ → меню: **Настройки** | **О программе** | (позже) Выход.

### 7.2. Настройки

Вкладки/секции (простой Win32, без «наворотов»):

1. **Профили** — список, добавить/изменить/удалить; поля host, port, database, user, password; кнопка **Тест**.
2. **Мониторинг** — PollInterval, WarningAfterMin, ErrorAfterMin.
3. **Звук** — частоты/длительности warning и error, gap.

Сохранение → файлы + `ReloadConfig` службе.

### 7.3. О программе

Имя продукта, версия (из `internal/version`), краткий копирайт.

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
- предложить/поставить трей в Run (HKCU/HKLM) — **уточнить** в §12, нужен ли автозапуск трея.

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
| `CHK-SQL-02` | ListPoints парсинг контрактных колонок |
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

### Фаза 1 — Config + модель

- `internal/config`, `internal/model`, DPAPI-секреты
- check: `CHK-CFG-01`

### Фаза 2 — SQL spike

- `internal/sqlcheck` + интерфейс reader; TestConnection; шаблон запроса
- **нужна схема/дамп или скрин полей от заказчика**
- checks: `CHK-SQL-01`, `CHK-SQL-02`

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

## 12. Открытые вопросы к заказчику (нужны ответы до/во время фазы 2)

1. **Точная SQL-схема** времён обмена в вашей базе 1С 8.1: имена таблиц/вьюх и полей отправки и приёма по точкам обмена (или тестовый доступ / скрипт `SELECT`).
2. Считать простой **по каждому направлению** (send/recv) отдельно или по **более свежей** из двух дат?
3. Часовой пояс: в SQL лежит локальное время сервера 1С или UTC? С чем сравниваем «сейчас»?
4. Нужен ли **Windows Authentication** к SQL или только SQL login/password?
5. Автозапуск **трея** вместе с логом пользователя — да/нет?
6. DPAPI: шифровать пароли для **LocalSystem** (служба) — тогда секреты в контексте машины/`SYSTEM`; трей при сохранении должен писать так, чтобы служба могла читать (типично `CRYPTPROTECT_LOCAL_MACHINE`).
7. Язык UI: только русский?
8. Несколько точек обмена в одной базе — все показывают в ЛКМ-списке (да, по умолчанию)?

---

## 13. Риски

| Риск | Митигация |
|------|-----------|
| Неизвестная схема таблиц 1С | Интерфейс reader + калибровка на реальной БД; SQL-шаблон в настройках |
| PC speaker на современных ПК без спикера | Beep всё равно зовём; параллельно можно дублировать `MessageBeep` — не вместо, а опция |
| Трей не запущен во время аварии | Очередь алертов в службе; при старте трея — повторная доставка |
| Go 1.21+ соблазн | Запрет POLICY; CI/check на версии в go.mod |
| Помощники sc/powershell | Только WinAPI SCM/Registry; static scan `CHK-POL-01` |

---

## 14. Что не делаем в v1

- Веб-морда, Electron, WPF, tray на сторонних UI-kit
- Уведомления по почте/Telegram
- Кластер из нескольких машин-агентов
- Сборка `.exe`/zip без явной просьбы заказчика
