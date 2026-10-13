# Фаза 7: Win32-инсталлятор Exchange Checker (план, утверждён 12.10.2026)

План написан EC Setup на `main` 56bba90 (7a реализована от `main` f892603) и утверждён координатором 12.10.2026 с решениями ниже. **Решения координатора (§D) имеют приоритет над текстом плана**; противоречащие места плана исправлены или помечены. Решения перенесены в PLAN.md §12 п. 90 (пп. 87–89 в `main` заняты решениями EC Tray T1). Статус подфаз: **7a — реализована** (ветка `phase7a`), **7b — реализована** (ветка `phase7b`), 7c–7e — запланированы.

## D. Решения координатора (12.10.2026)

1. **Один чекбокс «Запустить службу и запускать автоматически»**, по умолчанию включён: тип запуска AUTO (delayed) и `StartServiceW`; выключен — `SERVICE_DEMAND_START`, служба не запускается. POLICY §2 и PLAN §8/§9 сведены к этой формулировке.
2. **Delayed auto-start — да** (`SERVICE_CONFIG_DELAYED_AUTO_START_INFO = TRUE`).
3. **Failure actions:** перезапуск через 60 / 120 / 300 с, сброс счётчика 86400 с, `SERVICE_CONFIG_FAILURE_ACTIONS_FLAG = TRUE`.
4. **ProgramData:** `D:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)` — без BU, логи тоже (PLAN §12 п. 9 и DELIVERY.md исправлены). **Папка установки** — `D:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;FRFX;;;BU)`, защищённый DACL всегда.
5. **Publisher / CompanyName / LegalCopyright — пустые:** пустые строки в VERSIONINFO не пишутся, `Publisher` в Uninstall-ключ не пишется. Значок setup = `cmd/exchtray/exchtray.ico`.
6. **Версии:** релиз — тег `vX.Y.Z` (Version = `X.Y.Z`); CI — `0.0.0-ci.<run_number>`; даунгрейд — отказ с кодом 3, только `/ALLOWDOWNGRADE`; pre-release/dev — переустановка с предупреждением.
7. **Канал поставки (а):** CHK-CI-01 не меняется (артефакты не загружаются), zip собирается и проверяется внутри job; заказчику — по явной просьбе из среды нейросети. Пересмотр на фазе 8.
8. **Трей после установки/обновления** запускается без повышения в своём сеансе через токен `explorer.exe` + `CreateProcessWithTokenW` — это запуск процесса продукта, не helper-процесс (POLICY §4). Не вышло — сообщение «значок появится при следующем входе», не ошибка; в тихом режиме не запускается.
9. **Трей в чужих сеансах** — `TerminateProcess`; T2 (глобальное событие) не делаем. `TerminateProcess` зависшей службы — только с согласия пользователя, в тихом режиме — ошибка.
10. **`internal/winres` и `tools/mkres` — EC Setup.** EC Setup генерирует и коммитит `.syso` для `cmd/exchsetup` и `cmd/exchsvc` (MESSAGETABLE из `alert.EventIDs()`). S5 (`config.ServiceExeName`, `config.SetupExeName`, `config.UninstallExeName`) — EC Setup. S1 (`func EventIDs() []uint32` в `internal/alert/sink.go`, отсортированный срез 100..105, 110), S3 (`TestCHK_SVC_02_MessageTable`), S4 (контракт ReportEventW) — EC Service. T3 (трей на mkres) — 7e. Константу выхода трея `model.TrayExitMessage = "ExchangeChecker.TrayExit"` добавляет EC Tray (T1).
11. **Деинсталлятор C2:** отдельная маленькая сборка без payload `ExchangeCheckerUninstall.exe` в папке установки (DELIVERY.md).
12. Самоудаление: файл до перезагрузки (`MOVEFILE_DELAY_UNTIL_REBOOT`) + код 3010 — допустимо.
13. **Гейтированные E2E** (`EC_SETUP_E2E`, `EC_SVC_E2E`) могут трогать настоящий HKLM только на одноразовом windows-раннере и с очисткой (исключение записано в CHECKS.md рядом с CHK-CFG-04).
14. Только amd64. 15. Только LocalSystem.
16. **Чужой владелец каталога ProgramData** — рекурсивно владелец BA + DACL, предупреждение в журнал; защищённый DACL `secrets.dpapi` не ломать.
17. Защищённый DACL на папке установки — всегда.
18. Без подписи кода в v1 (упомянуто в DELIVERY.md).
19. Решение о сжатии payload — после 7b.
20. Язык описания службы и ярлыка = язык установки; ярлык только на трей.
21. Без `dpiAware` в манифесте setup.
22. Тег `payload` и отдельный `go vet -tags payload` — как в плане (§11 п. 22).
23. Ключ источника Event Log при удалении удаляется.
24. HKCU других пользователей при удалении остаются — как в плане.
25. Тесты с настоящей службой — только в отдельных гейтированных шагах — как в плане.

## R. Реализация 7a и отклонения от плана

- **MESSAGETABLE** — в `cmd/exchsvc/rsrc_windows_amd64.syso` (S2, решение 10). Пока шла 7a, EC Service влил в `main` S1 (`alert.EventIDs()`, 767c075) и S4 (контракт ReportEventW); ветка `phase7a` создана от `main` f892603, и `tools/mkres` берёт ID прямо из `alert.EventIDs()` — временный список не понадобился. `TestCHK_RES_01_MessageTableIDs` сверяет таблицу с `alert.EventIDs()`; полноту списка держит `TestCHK_SVC_02_EventIDs` (EC Service). S3 (`TestCHK_SVC_02_MessageTable`, FormatMessageW на windows) остаётся за EC Service.
- Язык ресурсов: MANIFEST / VERSION / ICON — 0x0409, MESSAGETABLE — LANG_NEUTRAL (§3.2).
- FILEVERSION: `X.Y.Z` → `X,Y,Z,0`; `X.Y.Z-ci.N` → `X,Y,Z,N`; любой pre-release ставит `VS_FF_PRERELEASE` (`winres.ParseVersion`); тег с `v` в `Version` не принимается.
- В `.syso` службы значка нет (VERSION + MANIFEST + MESSAGETABLE). У трея VERSIONINFO пока нет (akavel/rsrc) — появится с T3 в 7e; в 7a CHK-VER-01 для трея проверяет только, что `-X` попал в exe.
- Закоммиченные `.syso` — для `0.0.0-dev`; шаг CI перегенерирует их с `0.0.0-ci.<run_number>` только в рабочей копии job и возвращает обратно (`mkres -check` до и после).
- CHK-POL-01 сканирует и `tools/`.
- Проверка, что линкер Go 1.20.14 принимает `.syso` (`TestCHK_RES_01_LinkedExe`): ресурсы собранного exe побайтно равны ресурсам объекта.

## 0. Исходное состояние (факты из репозитория на 56bba90)

| Что | Где | Факт |
|---|---|---|
| Заглушка инсталлятора | `cmd/exchsetup/main.go` | `func main() { _ = version.Version }` |
| Версия | `internal/version/version.go` | `const Version = "0.0.0-dev"`, `const ProductName = "Exchange Checker"`. Флаг `-ldflags -X` с `const` **не работает** |
| Пакет инсталлятора | `internal/wininstall/doc.go` | есть только doc-комментарий |
| go.mod | `go.mod` | `go 1.20`, прямая зависимость одна: `github.com/microsoft/go-mssqldb v1.8.2`. **`golang.org/x/sys` в go.mod нет**: он есть только в `go.sum` (v0.21.0, транзитивно). `TestCHK_TRAY_10_TrayDeps` (`checks/tray10_test.go`) падает, если x/sys появится в go.mod. Значит, инсталлятор пишется только на `syscall` + `internal/winapi` |
| CI | `.github/workflows/ci.yml` | job `windows` (`windows-latest`, на GitHub-hosted runner процесс выполняется с правами администратора) и job `linux` (ubuntu + контейнер mssql). Артефакты не загружаются (CHK-CI-01). Трей собирается в `$RUNNER_TEMP/exchtray.exe` |
| Ресурсы exe | `cmd/exchtray/rsrc_windows_amd64.syso`, `exchtray.ico`, `exchtray.manifest`, `RESOURCES.md` | один `.syso`, только у трея. Собран внешней утилитой `akavel/rsrc@v0.10.2`, которую ставят через `go install` вне модуля. В нём только манифест и значок. **Генератора VERSIONINFO и MESSAGETABLE в репозитории нет.** `.rc`, `Makefile` и скриптов сборки нет. У службы нет ни `.syso`, ни манифеста |
| Имена | `internal/model/ipc.go`, `internal/config/autostart.go` | `model.ServiceName = "ExchangeChecker"`, `model.EventSource = "ExchangeChecker"`, `model.PipeName = \\.\pipe\ExchangeChecker`, `config.TrayExeName = "ExchangeCheckerTray.exe"`, `config.TrayRunValue = "ExchangeCheckerTray"`, `config.RunKeyPath`. **Констант для `ExchangeCheckerSvc.exe` и `ExchangeCheckerSetup.exe` нет** |
| Event Log | `internal/alert/sink.go`, `internal/winapi/eventlog_windows.go` | ID: 100 (служба запущена или остановлена, info), 101 (снижение уровня, info), 102 (warning), 103 (error), 104 (тревога подтверждена, info), 105 (конфигурация изменена через IPC, info), 110 (конфигурация или внутренняя ошибка). Тип у 110 — warning, а при ошибке создания канала в `cmd/exchsvc/app.go` — error. `ReportEventW` вызывается с категорией 0 и **одной строкой-вставкой**, dwEventID передаётся «голым» (100…110, без битов severity). Источник служба не регистрирует (PLAN §12 п. 34) |
| Жизненный цикл службы | `internal/winsvc/service.go` | RUNNING сообщается сразу после запуска функции службы. Принимаются Stop и Shutdown. STOP_PENDING повторяется каждые 2 с, WaitHint 30 с, рост ограничен 60 с (CHK-MON-14). Ошибка функции даёт `ERROR_SERVICE_SPECIFIC_ERROR`. Вне SCM служба работает в консоли (`ErrNotService`) |
| Автозапуск трея | `cmd/exchsvc/main.go`, `app.go` (`trayAutostarter`), `internal/config/autostart*.go` | Под SCM служба при **каждом** старте и после `save_settings` приводит `HKLM\…\Run\ExchangeCheckerTray` (64-битное представление) к `settings.trayAutostart` через `config.ApplyTrayAutostart` и `config.TrayRunCommand(os.Executable())`. Значение — `"<папка exe службы>\ExchangeCheckerTray.exe"` в кавычках. Без `settings.json` действует `model.DefaultSettings().TrayAutostart = true`. Если `settings.json` не читается, значение не трогается |
| ProgramData | `internal/config/config.go`, `atomic.go`, `atomic_windows.go`, `internal/alert/logfile.go` | `config.DefaultDir()` = `%ProgramData%\ExchangeChecker`. Каталог создаётся через `os.MkdirAll(dir, 0o755)`; на Windows биты прав игнорируются, и DACL **наследуется от %ProgramData%**. Файлы: `settings.json`, `profiles.json` (DACL наследуется), `secrets.dpapi` (явный `D:P(A;;FA;;;SY)(A;;FA;;;BA)` = `config.SecretsSDDL`), `counters-state.json`, `ack-state.json`, `logs\exchsvc.log` |
| Трей и файлы | `internal/wintray`, `internal/tray*`, `cmd/exchtray` | **Трей не импортирует `internal/config` и не пишет и не читает файлы в ProgramData** (CHK-TRAY-06: всё идёт через IPC). Язык трея хранится в `HKCU\Software\ExchangeChecker\Tray\Language` |
| Single-instance трея | `internal/wintray/single_windows.go`, `app_windows.go`, `settings_windows.go` | Мьютекс `Local\ExchangeCheckerTray`, свой в каждом сеансе. Скрытое окно класса `ExchangeCheckerTray`. Окно `--settings` — `ExchangeCheckerTraySettings` (свой мьютекс `SettingsMutexName`). **`WM_CLOSE` трей не обрабатывает**: срабатывает DefWindowProc → DestroyWindow → PostQuitMessage, а `App.Exit()` не вызывается, поэтому значок не удаляется (NIM_DELETE). Межсеансового механизма выхода нет |
| Проверки политики | `checks/pol01_test.go`, `checks/tray10_test.go` | CHK-POL-01: в `cmd/` и `internal/` нет строк `sc.exe`, `powershell`, `cmd.exe`, `wmic`, `net.exe`. CHK-TRAY-10: **каждый `NewProc("…")` в `cmd/` и `internal/`** обязан быть в allowlist с минимальной версией ≤ 6.1; вызовы только через `callProc`; без `MustLoadDLL` |

Требования к фазе 7, уже записанные в репозитории: DELIVERY.md («Требования к инсталлятору (фаза 7) из фазы 6»), PLAN §9, §11 «Фаза 7», §12 пп. 9, 34, 47, 56, 58, 59, 72; CHECKS: `CHK-SETUP-01` (planned), `CHK-SVC-01` (planned: «служба под настоящим SCM, источник Event Log»), `CHK-DELIV-01` (planned).

---

## R2. Реализация 7b

- Тихий режим: `internal/wininstall` (план + `System`), `cmd/exchsetup` (`/S`, `/uninstall`), payload `-tags payload` (svc+tray+uninstall C2).
- WinAPI: `internal/winapi/scm_*.go`, `path_windows.go`, `regextra_windows.go`, `owner_windows.go`.
- Проверки CHK-SETUP-01..07, CHK-SVC-01, базовый CHK-SETUP-12; CI — сборка с payload + `go vet -tags payload` + E2E на windows.
- Сжатие payload (решение координатора после 7b): **в 7b без сжатия**; **в 7e — gzip** через stdlib `compress/gzip` только (без внешних утилит). Замеры 7b (Go 1.20.14, -s -w, windows/amd64): svc 6.65 MiB, tray 2.68 MiB, uninstall 2.22 MiB, Setup with embed 13.79 MiB; gzip-9 ≈ 4.75 MiB (41%).

## 1. Мастер установки и доставка exe внутрь Setup.exe

### 1.1. Как служба и трей попадают в Setup.exe

| Вариант | Плюсы | Минусы |
|---|---|---|
| **A. `go:embed` + порядок сборки svc, tray → setup** | Чистый Go и std, CGO=0, без внешних утилит. Байты лежат в rodata образа и проверяются в тестах (sha256, VERSIONINFO вложенных exe). Работает одинаково на linux- и windows-раннере | Нужен порядок сборки. Без файлов `go build ./...` падает, поэтому нужен build tag |
| B. RCDATA-ресурс в `.syso` | Доступ штатным `FindResourceW`/`LoadResource`/`LockResource`/`SizeofResource` | Тот же порядок сборки плюс генерация `.syso` на 15–20 МБ при каждой сборке. Четыре лишних API. Пользы по сравнению с A нет |
| C. Payload, дописанный в конец exe | Не нужен рекомпайл setup. Копию-деинсталлятор можно «обрезать» | Хрупко: поиск трейлера в своём файле. Ломается при будущей подписи Authenticode (overlay). Эвристики антивирусов. Скрипт дописывания — ещё один инструмент |

**Выбор: A.** Схема:
- `cmd/exchsetup/payload_embed.go` с `//go:build payload` содержит `//go:embed payload/ExchangeCheckerSvc.exe payload/ExchangeCheckerTray.exe`.
- `cmd/exchsetup/payload_none.go` с `//go:build !payload` даёт пустой payload. Такая dev-сборка отказывается ставить продукт («сборка без файлов продукта», код 1), но может удалять (см. §6.2, вариант C2).
- В `.gitignore` добавить `cmd/exchsetup/payload/` (`*.exe` там уже игнорируется).
- Порядок сборки (CI, позже скрипт фазы 8), всё под `GOOS=windows GOARCH=amd64 CGO_ENABLED=0`, флаги `-trimpath -ldflags "-s -w -X …/internal/version.Version=$V"`:
  1. `go run ./tools/mkres -version $V`: перегенерировать `.syso` с версией (§8);
  2. `go build … -o cmd/exchsetup/payload/ExchangeCheckerSvc.exe ./cmd/exchsvc`;
  3. `go build … -H windowsgui -o cmd/exchsetup/payload/ExchangeCheckerTray.exe ./cmd/exchtray`;
  4. `go build -tags payload … -H windowsgui -o $OUT/ExchangeCheckerSetup.exe ./cmd/exchsetup`.
- Контроль целостности. Setup пишет файл через временный файл, затем перечитывает и сверяет sha256 с вложенными байтами. Тест сборки проверяет, что `ProductVersion` в VERSIONINFO вложенных svc и tray равна `version.Version` самого setup. Отдельный manifest.json не нужен: парсер ресурсов уже есть в `internal/winres` (§8).
- Сжатие (`compress/gzip`, std) — по желанию. Размер не измерялся: собирать exe в рамках этой задачи было запрещено. Решение принимается после первой сборки в 7b (см. «Риски»).

### 1.2. Код: где что лежит
- `internal/wininstall`. Логика без побочных эффектов: разбор командной строки, проверка пути, сравнение версий, **план шагов** (`[]Step`) для установки, обновления и удаления, значения Uninstall-ключа, SDDL-константы. Она собирается и тестируется на linux. Исполнитель шагов спрятан за интерфейсом (`Executor`): в тестах это фейк-рекордер, на Windows — реальный `*_windows.go`. Так выполняется формулировка CHK-SETUP-01 «dry-run/registry helpers».
- `internal/wininstall/*_windows.go`: SCM, реестр, ACL, файлы, процессы, COM-ярлык, окна мастера.
- Новые тонкие обёртки WinAPI — в `internal/winapi` отдельными файлами (`scm_windows.go`, `proc_windows.go`, `shell_windows.go`, `com_windows.go`), чтобы не конфликтовать с файлами EC Service и EC Tray. Каждый новый `NewProc` добавляется в allowlist `checks/tray10_test.go` (список в приложении A).
- GUI-хелперы трея (`internal/wintray/window_windows.go`, `layout_windows.go`) **не импортировать**: это зона EC Tray, и импорт затянул бы в setup тяжёлые зависимости. Похожие небольшие хелперы держать в `internal/wininstall`.
- Общие константы: `config.ServiceExeName = "ExchangeCheckerSvc.exe"` и `config.SetupExeName` рядом с `config.TrayExeName`.

### 1.3. UI мастера (чистый WinAPI)
- Одно окно верхнего уровня класса `ExchangeCheckerSetup` из `CreateWindowExW`: STATIC, BUTTON, EDIT, чекбоксы `BS_AUTOCHECKBOX`, клавиатура через `IsDialogMessageW`. Это тот же подход, что в окне настроек трея. Шаблонов диалогов нет: rc.exe недоступен.
- Страницы по PLAN §9:
  1. Приветствие (при обновлении: «Установлена версия X, будет обновлена до Y»).
  2. Папка: EDIT, по умолчанию `%ProgramFiles%\ExchangeChecker`. Процесс 64-битный, поэтому это `C:\Program Files`; брать через `SHGetFolderPathW(CSIDL_PROGRAM_FILES)`. Кнопка «Обзор…» вызывает **`SHBrowseForFolderW`** (`BIF_RETURNONLYFSDIRS|BIF_NEWDIALOGSTYLE|BIF_EDITBOX|BIF_VALIDATE`, перед этим `OleInitialize`), затем `SHGetPathFromIDListW` и `CoTaskMemFree`. `IFileDialog` (Vista+) тоже работает на Win7, но через COM-vtable кода больше, а пользы мало. При обновлении папка не меняется (поле только для чтения).
  3. Опции: «Ярлык в меню Пуск».
  4. Прогресс: STATIC с текущим шагом плюс `msctls_progress32` (`InitCommonControlsEx`, comctl32 v6 по манифесту). Шаги выполняет рабочая горутина на `LockOSThread` (COM для ярлыка), прогресс уходит в окно через `PostMessageW(WM_APP+n)`. Отмена во время шагов не предусмотрена; при ошибке выполняется откат (§7.4).
  5. «Готово»: чекбокс службы (см. §2.4 и «Открытые вопросы», п. 1). Если сбой — текст ошибки и путь к журналу установки.
- Проверка папки: абсолютный путь, локальный фиксированный диск (`GetDriveTypeW == DRIVE_FIXED`, без UNC), не внутри `%ProgramData%\ExchangeChecker`, не корень диска, длина с запасом под имена файлов, свободное место (`GetDiskFreeSpaceExW`).
- Язык RU/EN: по `GetUserDefaultUILanguage` (`winapi.LanguageFromLANGID`), иначе RU; явно — ключ `/LANG=ru|en`. Каталог строк лежит в `internal/wininstall` (ключи `setup.*`), с тестом полноты RU/EN по образцу CHK-TRAY-08. Каталог трея `internal/i18n` (зона EC Tray) не трогаем.
- Права: манифест `requireAdministrator` (§8) и проверка во время работы `winapi.ProcessIsAdmin()` (на случай отключённого UAC у обычного пользователя).
- Один экземпляр: мьютекс `Global\ExchangeCheckerSetup`.
- **Тихий режим, нужен для CI и администраторов.** Окна и MessageBox не создаются, результат — код выхода и журнал.
  - Установка: `/S`, `/DIR=<путь>`, `/NOSTART` (не запускать службу), `/NOSHORTCUT`, `/LOG=<файл>`, `/ALLOWDOWNGRADE`, `/LANG=`.
  - Удаление: `/uninstall [/S] [/PURGEDATA]`.
  - Коды выхода: 0 — успех; 1 — ошибка; 2 — отмена пользователем; 3 — даунгрейд запрещён; 3010 — нужна перезагрузка (`ERROR_SUCCESS_REBOOT_REQUIRED`, если удаление отложено до перезагрузки).
  - Разбор строки свой: `flag` не подходит, он пишет usage в stderr GUI-процесса. Unit-тесты на linux.
- Журнал установки: `%TEMP%\ExchangeCheckerSetup-<дата>.log` или файл из `/LOG=`. Секретов в нём нет, их и нет в работе установщика.

---

## 2. Регистрация службы `ExchangeChecker`

### 2.1. CreateServiceW
- `OpenSCManagerW(nil, nil, SC_MANAGER_CREATE_SERVICE|SC_MANAGER_CONNECT)`.
- `CreateServiceW(scm, "ExchangeChecker" /*model.ServiceName*/, "Exchange Checker" /*version.ProductName*/, SERVICE_CHANGE_CONFIG|SERVICE_START|SERVICE_STOP|SERVICE_QUERY_STATUS|SERVICE_QUERY_CONFIG|READ_CONTROL|WRITE_DAC|DELETE, SERVICE_WIN32_OWN_PROCESS, <start type>, SERVICE_ERROR_NORMAL, "\"<InstallDir>\ExchangeCheckerSvc.exe\"", nil, nil, nil, nil /*LocalSystem*/, nil)`.
- ImagePath **в кавычках**, иначе при пробелах в пути получается уязвимость «unquoted service path». Без аргументов: служба по умолчанию берёт `config.DefaultDir()`.
- Учётная запись — LocalSystem: на ней держатся DPAPI LocalMachine, запись в HKLM Run и `ImpersonateNamedPipeClient` (PLAN §12 пп. 9, 57, 72).
- Если служба уже существует (обновление или повтор), вместо создания: `OpenServiceW` + `ChangeServiceConfigW(SERVICE_NO_CHANGE …, lpBinaryPathName, lpDisplayName)`. Тип запуска при обновлении **не трогаем**: пользователь мог поменять его в services.msc.

### 2.2. ChangeServiceConfig2W
- `SERVICE_CONFIG_DESCRIPTION` (1): `SERVICE_DESCRIPTIONW{lpDescription}` на языке установки. Например, RU: «Exchange Checker: контроль времени обмена 1С по базам MS SQL; тревоги в трее и журнале событий». EN — аналогично.
- `SERVICE_CONFIG_FAILURE_ACTIONS` (2): `SERVICE_FAILURE_ACTIONSW{dwResetPeriod=86400, lpRebootMsg=nil, lpCommand=nil, cActions=3, lpsaActions=[{SC_ACTION_RESTART,60000},{SC_ACTION_RESTART,120000},{SC_ACTION_RESTART,300000}]}`. Для `SC_ACTION_RESTART` дескриптор должен иметь `SERVICE_START`. Параметры требуют решения (Открытые вопросы, п. 3).
- `SERVICE_CONFIG_FAILURE_ACTIONS_FLAG` (4, Vista+): `fFailureActionsOnNonCrashFailures=TRUE`. Служба, у которой функция завершилась ошибкой, останавливается с `ERROR_SERVICE_SPECIFIC_ERROR` (`internal/winsvc/service.go`), и без этого флага перезапуск не сработает.
- `SERVICE_CONFIG_DELAYED_AUTO_START_INFO` (3, Vista+): **рекомендую `TRUE`** (Automatic, Delayed Start). Сразу после загрузки сеть и SQL часто ещё недоступны. Есть антидребезг в 2 опроса (§12 п. 33), но при отложенном старте меньше ложных error-событий. Цена — служба начинает работу примерно через 2 минуты после загрузки; трей переподключается сам (CHK-IPC-01). Требует решения (п. 2).
- Раскладка структур для amd64 закрывается тестом размеров по образцу `TestCHK_TRAY_10_StructSizes`: `SERVICE_FAILURE_ACTIONSW` — 40 байт, смещения 0/8/16/24/32; `SC_ACTION` — 8 байт; `SERVICE_DESCRIPTIONW`, `SERVICE_DELAYED_AUTO_START_INFO`, `SERVICE_FAILURE_ACTIONS_FLAG` — по одному полю.

### 2.3. DACL службы (SetServiceObjectSecurity)
- Типичный DACL новой службы на Win7 (`sc sdshow`) приведён **по документации и опыту, а не по репозиторию**. windows-тест обязан сначала прочитать фактический DACL через `QueryServiceObjectSecurity` и записать его в лог:
  `D:(A;;CCLCSWRPWPDTLOCRRC;;;SY)(A;;CCDCLCSWRPWPDTLOCRSDRCWDWO;;;BA)(A;;CCLCSWLOCRRC;;;IU)(A;;CCLCSWLOCRRC;;;SU)`.
  У IU здесь уже есть `LC` = `SERVICE_QUERY_STATUS`.
- **Предлагаемая строка** (DELIVERY.md: QUERY_STATUS для IU и AU, чтобы трей сверял PID по G7):
  ```
  D:(A;;CCLCSWRPWPDTLOCRRC;;;SY)(A;;CCDCLCSWRPWPDTLOCRSDRCWDWO;;;BA)(A;;CCLCSWLOCRRC;;;IU)(A;;CCLCSWLOCRRC;;;SU)(A;;CCLCSWLORC;;;AU)
  ```
  - Расшифровка прав: CC = QUERY_CONFIG, LC = QUERY_STATUS, SW = ENUMERATE_DEPENDENTS, LO = INTERROGATE, RC = READ_CONTROL, RP/WP = START/STOP (только SY и BA), CR = USER_DEFINED_CONTROL.
  - AU получает чтение без CR, без запуска и остановки. Трею (`winapi.ServiceProcessID`) нужны только `SC_MANAGER_CONNECT` и `SERVICE_QUERY_STATUS`.
- Применение: `ConvertStringSecurityDescriptorToSecurityDescriptorW` (уже есть, `winapi.SecurityAttributesFromSDDL`), затем `SetServiceObjectSecurity(h, DACL_SECURITY_INFORMATION, sd)`. SACL не трогаем: для него нужен `SeSecurityPrivilege`, поэтому S: в строке нет. Применяется при установке и при каждом обновлении (идемпотентно).
- Тест: токен с BA как deny-only (`winapi.DenyOnlyToken`, `ImpersonateLoggedOnUser`) может `OpenServiceW(SERVICE_QUERY_STATUS)`, но получает отказ на `SERVICE_START`.

### 2.4. Запуск
- Чекбокс на «Готово» (POLICY §2: «галочка автозапуска на экране «Готово»»; PLAN §9: «Запустить службу после установки»). **Формулировки противоречат друг другу, нужно решение** (п. 1). **Решение 1:** один чекбокс, по умолчанию включён: «Запустить службу и запускать автоматически».
  - Включён: тип AUTO (delayed), затем `StartServiceW`.
  - Выключен: `SERVICE_DEMAND_START`, служба не запускается.
- После `StartServiceW` опрашивать `QueryServiceStatusEx` до `SERVICE_RUNNING` с таймаутом 60 с. Так как winsvc сообщает RUNNING почти сразу, дальше ещё ≈3 с проверять, что служба не перешла в STOPPED. При ошибке показать `Win32ExitCode`/`ServiceSpecificExitCode` и подсказку про журнал `%ProgramData%\ExchangeChecker\logs\exchsvc.log`.

---

## 3. Источник Event Log `ExchangeChecker`

### 3.1. Реестр
- Ключ `HKLM\SYSTEM\CurrentControlSet\Services\EventLog\Application\ExchangeChecker` (ветка SYSTEM не перенаправляется WOW64):
  - `EventMessageFile` — REG_EXPAND_SZ `<InstallDir>\ExchangeCheckerSvc.exe` (без кавычек);
  - `TypesSupported` — REG_DWORD `7` (ERROR|WARNING|INFORMATION).
- `CategoryMessageFile` и `CategoryCount` не нужны: категория всегда 0.
- В `internal/winapi` нужны новые обёртки `RegSetDWORDView` и `RegSetExpandStringView`; сейчас есть только REG_SZ (`RegSetStringView`).
- При деинсталляции ключ удаляется (`RegDeleteKeyExW`). Если данные сохранены, старые события в журнале будут показываться с преамбулой «описание не найдено». Это стандартное поведение.

### 3.2. Где держать таблицу сообщений
- **MESSAGETABLE-ресурс в `ExchangeCheckerSvc.exe`** (зона EC Service). Файл сообщений должен жить, пока стоит продукт, и лежать рядом со службой.
- Отдельная resource-only DLL без CGO и линкера потребовала бы своего PE-писателя, пользы от этого нет. Таблица в setup или в деинсталляторе — лишняя связность.
- Генерация без mc.exe. Пакет `internal/winres` (std-only, §8) пишет `RT_MESSAGETABLE` (тип 11, id 1) в формате `MESSAGE_RESOURCE_DATA`:
  - `NumberOfBlocks`, затем `MESSAGE_RESOURCE_BLOCK{LowId, HighId, OffsetToEntries}`;
  - затем `MESSAGE_RESOURCE_ENTRY{Length uint16, Flags uint16 = 1 (Unicode), Text UTF-16LE с NUL, выравнивание до 4}`.
  - Два блока: `[100..105]` и `[110..110]`.
  - Язык `LANG_NEUTRAL` (0x0000): `FormatMessage` с языком 0 ищет нейтральный первым.
- **Формат текста: каждый ID = `%1\r\n`** (ровно то, что mc.exe делает из `%1`).
  - Служба уже передаёт полный локализованный текст одной вставкой (`alert.Format`, `FormatAck`, `FormatConfigChange`, `fmt.Sprintf("%s %s started"…)` на языке `settings.language`). Любой фиксированный заголовок в таблице был бы на одном языке и дублировал бы текст.
  - **ID в таблице должны точно совпадать с dwEventID** (100…105, 110 без битов severity, customer и facility): просмотрщик ищет полный 32-битный ID. Если «по привычке» добавить severity, как это делает mc, ничего не найдётся. Это проверяется тестом.
- Смысл событий по коду (для комментария в исходнике таблицы и для DELIVERY):

  | ID | Тип | Источник в коде | Смысл |
  |---|---|---|---|
  | 100 | info | `cmd/exchsvc/app.go` (`EventIDService`) | «ExchangeChecker <версия> started» / «ExchangeChecker stopped» |
  | 101 | info | `alert.EventTypeOf` (`EventIDRecover`) | уровень снизился (восстановление) |
  | 102 | warning | `EventIDWarning` | уровень поднялся до warning |
  | 103 | error | `EventIDError` | уровень поднялся до error |
  | 104 | info | `OnAck` → `alert.FormatAck` | тревога подтверждена из трея |
  | 105 | info | `OnConfigChange` → `alert.FormatConfigChange` | конфигурация изменена администратором через IPC (учётная запись, действие, profileId) |
  | 110 | warning / error | `Warn`, автозапуск, канал IPC (`EventIDInternal`) | сбой загрузки конфигурации, зависший ридер, ошибка HKLM Run (warning); не создан канал IPC (error) |

### 3.3. Что нужно от EC Service (подфаза 7a)
- **S1.** Единый список ID: `alert.EventIDs() []uint32` (или экспортированный срез) в `internal/alert/sink.go`. Генератор таблицы и тест берут ID оттуда; новый ID без строки в таблице ломает тест.
- **S2 (решение 10: делает EC Setup, сделано в 7a).** `cmd/exchsvc/rsrc_windows_amd64.syso`, собранный общим генератором: `RT_MESSAGETABLE` + `RT_VERSION` + `RT_MANIFEST` (asInvoker, supportedOS Win7–10, без comctl). Рядом — исходники (`exchsvc.manifest`, при желании значок) и заметка о пересборке, как в `cmd/exchtray/RESOURCES.md`.
- **S3.** Тест `TestCHK_SVC_02_MessageTable`:
  - windows: `.syso` пакета линкуется и в тестовый бинарник `cmd/exchsvc`, поэтому `FormatMessageW(FORMAT_MESSAGE_FROM_HMODULE|FORMAT_MESSAGE_ARGUMENT_ARRAY, GetModuleHandleW(nil), id, 0, …, ["x"])` для каждого ID из S1 должен вернуть `x`;
  - linux: статический разбор `.syso`.
- **S4.** Зафиксировать контракт: `ReportEventW` с категорией 0, одной вставкой и ID без старших битов. Сейчас так и есть; закрепить комментарием и тестом.
- **S5 (решение 10: EC Setup, сделано в 7a).** `config.ServiceExeName`, `config.SetupExeName`, `config.UninstallExeName` (`internal/config/install.go`).

---

## 4. ACL `%ProgramData%\ExchangeChecker`

### 4.1. Предлагаемый DACL
```
D:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)
```
- **Решение 4: без BU** (логи тоже). Исходное предложение плана было `…(A;OICI;FRFX;;;BU)`; оно отличалось от строки из задания: у BU **`FRFX`** (0x1200a9, стандартное «Чтение и выполнение») вместо `FR`. `FR` на каталоге даёт просмотр и чтение, но не `FILE_TRAVERSE`. Обычно обход разрешён привилегией bypass-traverse, но лучше повторить стандартную маску.
- Применение: `winapi.SetFileDACL(dir, sddl)`. Функция уже есть (`internal/winapi/token_windows.go`); при префиксе `D:P` она ставит `PROTECTED_DACL_SECURITY_INFORMATION`, `D:PAI` под это попадает. Плюс владелец каталога — BA (`SetNamedSecurityInfoW(OWNER_SECURITY_INFORMATION)`).
- Порядок: `MkdirAll`, DACL, только потом создание и запуск службы. При обновлении DACL применяется повторно (идемпотентно).

### 4.2. Сверка с тем, как служба создаёт файлы
- `SetNamedSecurityInfoW` на каталоге распространяет наследуемые ACE на существующих потомков, **кроме объектов с защищённым DACL**. `secrets.dpapi` (`D:P(A;;FA;;;SY)(A;;FA;;;BA)`, `config.SecretsSDDL`, CHK-CFG-03) остаётся только SY+BA. Это обязательный windows-тест.
- Новые файлы службы (`writeFileAtomic`: `os.OpenFile` + `os.Chmod`, на Windows это только атрибут read-only; `alert.OpenRotatingFile`, `MkdirAll(logs)`) наследуют SY:F, BA:F, ~~BU:RX~~ (**BU отменено решением 4**: только SY и BA). ~~Это совпадает с PLAN §12 п. 9: `profiles.json` и `settings.json` доступны Users на чтение.~~ Users файлы каталога не читают (PLAN §12 п. 90.4).
- Временный файл секретов создаётся с явным SDDL (`winapi.CreateFileSDDL`) и не страдает.
- Кто пишет: только служба (LocalSystem = SY:F). **Трей ничего не пишет и даже не читает в ProgramData** (не импортирует `internal/config`, CHK-TRAY-06); повышенный `--settings` тоже пишет через службу по IPC. **Конфликта записи нет.** Консольный запуск `exchsvc` администратором тоже работает (BA:F).
- **Наблюдение, требует решения (Открытые вопросы, п. 4).** Раз трей с фазы 6 файлы не читает, чтение для Users функционально не нужно. При этом `profiles.json` содержит `manualSql` и полный mapping, которые IPC отдаёт только администратору (`get_config`, PLAN §12 п. 57). Через ACL файла Users прочитают их напрямую, и ограничение IPC теряет смысл. Варианты:
  - (а) оставить BU-read, как записано в п. 9 и DELIVERY;
  - (б) `D:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)`, тогда нужно поправить PLAN §12 п. 9 и DELIVERY.md.
- Захват каталога заранее. По умолчанию в `%ProgramData%` Users могут создавать подкаталоги, так что каталог мог создать обычный пользователь до установки. Владелец таких файлов сохраняет неявные права WRITE_DAC. Рекомендация: при установке сделать владельцем BA каталог и (обходом дерева) все существующие файлы. См. «Риски».

### 4.3. ACL папки установки
- Папка вне `Program Files` (например, `C:\ExchangeChecker`) наследует ACL корня диска, где у Authenticated Users есть право изменения. Тогда пользователь может подменить exe службы, работающей как LocalSystem.
- Рекомендация: **всегда** ставить на папку установки `D:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;FRFX;;;BU)` с владельцем BA. Тест: обычный пользователь не может писать в папку.

---

## 5. HKLM Run `ExchangeCheckerTray`

- Разрядность: сборка только amd64 (DELIVERY). Процесс setup 64-битный, `KEY_WOW64_64KEY` для него ничего не меняет, но указываем его явно, как в службе (`config.MachineRunRegistry()`). Uninstall-ключ тоже пишется в 64-битное представление. На 32-битной Win7 amd64-exe не запустится — это граница поставки (п. 14).
- **Единый источник истины — код службы.** Инсталлятор **не формирует строку сам**, а вызывает:
  ```
  cmd, _ := config.TrayRunCommand(filepath.Join(installDir, config.ServiceExeName))
  st, err := config.NewStore("").LoadSettings()     // только чтение
  if err == nil { config.ApplyTrayAutostart(config.MachineRunRegistry(), st.TrayAutostart, cmd) }
  // err != nil (settings.json не читается/битый) -> значение не трогаем, как служба (CHK-CFG-04)
  ```
  Строка ImagePath службы (без кавычек) и путь в `TrayRunCommand` — **одна и та же строка**. Служба получает путь через `os.Executable()` → `GetModuleFileNameW`, это путь из ImagePath. Значит, при первом старте служба увидит «совпадает, не трогать» (`AutostartUnchanged`). Если нормализация где-то разойдётся (регистр, 8.3), служба один раз перепишет значение; это безвредно и идемпотентно.
- Свежая установка: `settings.json` нет → `DefaultSettings().TrayAutostart = true` → значение пишется.
- Обновление: если пользователь выключил автозапуск (`trayAutostart:false`), `ApplyTrayAutostart` удалит значение или оставит его отсутствующим. **Ничего не перезаписывается против воли пользователя.**
- Настройки нечитаемы → значение не меняется (то же правило, что у службы).
- Гонки нет: служба пишет только при своём старте и после `save_settings`. На время обновления служба остановлена, после него запускается и сверяет то же значение.
- Деинсталляция: **после** остановки службы вызвать `winapi.RegDeleteValueView(HKLM, RunKeyPath, TrayRunValue, KEY_WOW64_64KEY)`; отсутствующее значение не считается ошибкой. Если удалить раньше, работающая служба вернёт значение только при `save_settings`, но порядок всё равно фиксируем.
- Импорт `internal/config` в setup не тянет SQL-драйвер: config импортирует только `internal/model` и `internal/winapi`. Это проверяется тестом зависимостей (CHK-SETUP-11).

---

## 6. Uninstall-запись, деинсталлятор, ярлыки

### 6.1. `HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\ExchangeChecker` (64-битное представление)

| Значение | Тип | Содержимое |
|---|---|---|
| DisplayName | REG_SZ | `Exchange Checker` (`version.ProductName`) |
| DisplayVersion | REG_SZ | `version.Version` |
| Publisher | REG_SZ | **в репозитории не задан**: решение (п. 5) |
| InstallLocation | REG_SZ | `<InstallDir>` |
| UninstallString | REG_SZ | `"<InstallDir>\<деинсталлятор>.exe" /uninstall` |
| QuietUninstallString | REG_SZ | `"<InstallDir>\<деинсталлятор>.exe" /uninstall /S` |
| DisplayIcon | REG_SZ | `<InstallDir>\ExchangeCheckerTray.exe,0` (значок есть в .syso трея) |
| EstimatedSize | REG_DWORD | сумма размеров файлов в КиБ |
| NoModify / NoRepair | REG_DWORD | 1 / 1 |
| InstallDate | REG_SZ | `YYYYMMDD` (необязательно) |

Ключ пишется последним при установке и удаляется последним при деинсталляции: после сбоя посередине пользователь может повторить удаление.

### 6.2. Деинсталлятор
- Варианты:
  - **C1 (рекомендую для 7b):** при установке setup копирует **сам себя** (`os.Executable()`) в `<InstallDir>\ExchangeCheckerUninstall.exe`. Код один, удаление запускается с `/uninstall`. Минус: копия несёт payload (лишние мегабайты на диске; размер не измерен).
  - C2: в setup вкладывается третий файл — сборка того же `cmd/exchsetup` **без** тега `payload` (небольшой исполняемый файл, который умеет только удалять). Добавляется шаг сборки.
  - Имя файла и сам факт появления 4-го файла в папке установки нужно отразить в DELIVERY.md (п. 11).
- Шаги удаления (план шагов; каждый идемпотентен, «отсутствует» = успех):
  1. Проверка прав администратора, мьютекс, диалог подтверждения с чекбоксом «Удалить настройки и пароли (%ProgramData%\ExchangeChecker)». **По умолчанию выключен** (PLAN §9: «по умолчанию настройки оставляем»). В тихом режиме данные удаляются только с `/PURGEDATA`.
  2. Остановить службу: `ControlService(SERVICE_CONTROL_STOP)`, ждать STOPPED до 90 с (CHK-MON-14: WaitHint 30 с, рост checkpoint ограничен 60 с), затем ждать выхода процесса по PID из `QueryServiceStatusEx` (`OpenProcess(SYNCHRONIZE)` + `WaitForSingleObject`). Потом `DeleteService`.
  3. Завершить трей и окна `--settings` во всех сеансах (§7.3).
  4. Удалить значение HKLM Run (§5).
  5. Удалить ключ источника Event Log (§3.1).
  6. Удалить ярлыки (§6.3) и вызвать `SHChangeNotify`.
  7. Удалить `ExchangeCheckerSvc.exe` и `ExchangeCheckerTray.exe`.
  8. Удалить сам деинсталлятор без helper-процесса:
     - (а) **предпочтительно:** если деинсталлятор запущен из папки установки, переименовать **работающий** exe через `MoveFileExW` (без `MOVEFILE_COPY_ALLOWED`) в `%SystemRoot%\Temp\ExchangeChecker-uninst-<rand>.exe`. Переименование запущенного образа в пределах тома разрешено. Затем `MoveFileExW(tmp, NULL, MOVEFILE_DELAY_UNTIL_REBOOT)`. Папка установки становится пустой и удаляется сразу.
     - (б) Если тома разные: `MOVEFILE_DELAY_UNTIL_REBOOT` для самого exe и для пустой папки (порядок: сначала файл, потом папка). Код 3010.
     - Не используем: перезапуск копии из `%TEMP%` (это новый процесс, серая зона POLICY) и трюк с переименованием потока и `FILE_DISPOSITION` (недокументированное поведение, на разных сборках Windows работает по-разному).
  9. С `/PURGEDATA` или отмеченным чекбоксом: `os.RemoveAll(%ProgramData%\ExchangeChecker)` (служба уже остановлена) и `HKCU\Software\ExchangeChecker` **текущего** пользователя. HKCU других пользователей остаются (ограничение).
  10. Удалить Uninstall-ключ.

### 6.3. Ярлыки в меню Пуск
- Путь: `SHGetFolderPathW(CSIDL_COMMON_PROGRAMS=0x17)` → `\Exchange Checker\Exchange Checker.lnk`, цель — `ExchangeCheckerTray.exe`, рабочая папка — папка установки, значок — индекс 0 в exe трея. Ярлык на `--settings` — по желанию (п. 20).
- Реализация: COM без CGO, через `syscall.SyscallN` по vtable (есть в Go 1.18+).
  - `CoInitializeEx(STA)` на рабочем потоке с `LockOSThread`.
  - `CoCreateInstance(CLSID_ShellLink {00021401-0000-0000-C000-000000000046}, nil, CLSCTX_INPROC_SERVER, IID_IShellLinkW {000214F9-0000-0000-C000-000000000046})`.
  - Индексы vtable `IShellLinkW`: 7 SetDescription, 9 SetWorkingDirectory, 11 SetArguments, 17 SetIconLocation, 20 SetPath.
  - Затем `QueryInterface(IID_IPersistFile {0000010B-0000-0000-C000-000000000046})`, индекс 6 Save(path, TRUE). Release для всех интерфейсов.
- Оценка сложности: средняя, порядка 150–250 строк. Главный риск — ошибиться в индексах vtable. Его закрывает windows-тест: создать `.lnk` во временной папке, загрузить через `IPersistFile::Load` и сверить `GetPath` (3), `GetArguments` (10), `GetIconLocation` (16).
- Альтернативы хуже:
  - писать бинарный `.lnk` по MS-SHLLINK самим — хрупко;
  - `.url` даёт неправильные значок и поведение;
  - без ярлыков — нарушение POLICY §2 («ярлыки»).
- Вызовы по vtable — не `LazyProc`. Их нужно оформить как документированное исключение из правила `callProc` в CHK-TRAY-10/CHK-SETUP-11. Сами `CoCreateInstance` и прочие — через `NewProc` + `callProc`.

---

## 7. Обновление поверх установленной версии

### 7.1. Обнаружение
- Читаем Uninstall-ключ (`DisplayVersion`, `InstallLocation`). Запасной путь — `QueryServiceConfigW` для `ExchangeChecker` (ImagePath → папка).
- Если ключа нет, а служба есть (установка сломана), считаем это обновлением в папку из ImagePath.
- Папку при обновлении не меняем. Перенос установки — через удаление и новую установку.

### 7.2. Политика версий
- Версии сравниваются по semver.
  - Новая > установленной: обновление.
  - Равны: переустановка (перезапись файлов и повторное применение всех регистраций).
  - **Новая < установленной: отказ** (в UI — сообщение, в тихом режиме — код 3), разрешить можно только `/ALLOWDOWNGRADE`. Причина: `config.SchemaVersion` и форматы файлов могут вырасти, и старая служба не прочитает новые файлы.
- Если любая из версий pre-release или dev (`0.0.0-dev`, `-ci.N`), считаем это переустановкой и пишем предупреждение в журнал (нужно CI). Требует решения (п. 6).

### 7.3. Остановка службы и трея
1. Запомнить состояние службы (RUNNING или нет) и тип запуска.
2. Остановить службу, как в §6.2 шаг 2 (STOP, до 90 с, ожидание процесса). Если служба зависла, спросить пользователя (в тихом режиме — ошибка); `TerminateProcess` для службы — только после явного согласия (п. 9).
3. Трей:
   - найти процессы через `CreateToolhelp32Snapshot` + `Process32FirstW/NextW` по имени, затем `OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION|SYNCHRONIZE|PROCESS_TERMINATE)` + `QueryFullProcessImageNameW` (6.0) с проверкой, что путь внутри папки установки;
   - **свой сеанс** (`ProcessIdToSessionId`): найти окна классов `ExchangeCheckerTray` и `ExchangeCheckerTraySettings`/`…Discovery` через `FindWindowExW` + `GetWindowThreadProcessId`, отправить `PostMessageW(hwnd, RegisterWindowMessageW("ExchangeChecker.TrayExit"))` (**нужна доработка EC Tray T1**) и ждать выхода процесса до 5 с. UIPI пропускает сообщения от процесса с более высоким уровнем целостности к процессу с более низким;
   - **другие сеансы** (RDP, несколько пользователей) или процесс, не вышедший за 5 с: `TerminateProcess`. В другом сеансе значок трея останется «призраком», пока на него не наведут мышь. Для v1 это допустимо. Альтернатива — T2 (глобальное событие), см. п. 9.
   - Почему не `WM_CLOSE`: сейчас он закрывает трей без `App.Exit()`, и значок не удаляется, а окно настроек на `WM_CLOSE` спросит «Отменить изменения?» (§12 п. 67) и зависнет в ожидании ответа.
   - Restart Manager (`rstrtmgr.dll`, Vista+) рассмотрен и отклонён: сложнее, а поведение между сеансами неочевидно.

### 7.4. Замена файлов с откатом
- Для каждого exe:
  1. новый файл пишется во временный в той же папке, затем `MoveFileExW(REPLACE_EXISTING|WRITE_THROUGH)` с повтором при ACCESS_DENIED/SHARING_VIOLATION/LOCK_VIOLATION. Логика та же, что `config.WriteFileAtomic`; её можно переиспользовать напрямую, `perm` на Windows игнорируется;
  2. перед заменой старый файл копируется в `*.bak`;
  3. если файл держит процесс, который не удалось завершить: переименовать старый в `*.old` и запланировать удаление через `MOVEFILE_DELAY_UNTIL_REBOOT`, код 3010.
- Ошибка на любом шаге: вернуть `*.bak`, восстановить прежние значения реестра (журнал шагов), запустить старую службу, если она работала.
- Свежая установка при ошибке откатывает сделанные шаги в обратном порядке.

### 7.5. Конфигурация и завершение
- `%ProgramData%\ExchangeChecker` **не трогается**, кроме повторного применения DACL (§4). Тест сверяет хэши файлов до и после.
- Обновить DisplayVersion, EstimatedSize, ImagePath (`ChangeServiceConfigW`), описание, failure actions, DACL службы и источник Event Log. HKLM Run обновить по §5. Тип запуска сохранить.
- Запустить службу, если она работала до обновления (в UI чекбокс по умолчанию отражает прежнее состояние).
- Трей сам не поднимется до следующего входа. Запуск трея без повышения прав в своём сеансе — см. п. 8.

---

## 8. Версия, VERSIONINFO, манифесты, генерация .syso

### 8.1. Версия
- `internal/version/version.go`: `const Version` → `var Version = "0.0.0-dev"`. `ProductName` остаётся константой.
- Использования (`cmd/exchsvc/main.go`, `app.go`, `internal/trayui/about.go`, тесты) — только в выражениях времени выполнения, поэтому замена безопасна.
- Сборка: `-ldflags "-X github.com/alexal46372-maker/ExchangeChecker/internal/version.Version=$V"`.
- Источник `$V` (п. 6):
  - релиз — тег `vX.Y.Z`;
  - обычный CI — `0.0.0-ci.<run_number>`.
- Для VERSIONINFO: `X.Y.Z` → `X,Y,Z,0`, pre-release и dev → `0,0,0,0` (или `0,0,0,<run_number>`).

### 8.2. Генератор ресурсов в репозитории (чистый Go, std-only)
- **`internal/winres`** (библиотека, собирается на всех ОС) пишет COFF-объект amd64:
  - `IMAGE_FILE_HEADER` (Machine 0x8664), одна секция `.rsrc`, таблица символов с символом секции;
  - **релокации `IMAGE_REL_AMD64_ADDR32NB` (3)** на каждое `IMAGE_RESOURCE_DATA_ENTRY.OffsetToData`. Так делает akavel/rsrc, и это ожидает загрузчик `.syso` в Go-линкере;
  - ресурсы: `RT_ICON` (3) и `RT_GROUP_ICON` (14) из `.ico`; `RT_MANIFEST` (24, id 1); `RT_VERSION` (16, id 1: `VS_VERSIONINFO` + `VS_FIXEDFILEINFO` + `StringFileInfo` + `VarFileInfo`); `RT_MESSAGETABLE` (11, id 1).
- В тот же пакет входит парсер ресурсов (для тестов и проверок в CI): `debug/pe` + разбор дерева ресурсов.
- **`tools/mkres`** (main, вне `cmd/` и `internal/`, не входит в граф продукта): `go run ./tools/mkres -version $V`. Перегенерирует `cmd/exchsetup/rsrc_windows_amd64.syso`, `cmd/exchsvc/rsrc_windows_amd64.syso` и, если EC Tray согласится, `cmd/exchtray/rsrc_windows_amd64.syso`. Описания целей (манифест, значок, FileDescription, OriginalFilename, таблица сообщений из `alert.EventIDs()`) жёстко прописаны в tools/mkres.
- Почему не внешние инструменты:
  - `akavel/rsrc` (уже используется для трея) умеет только манифест и значок;
  - `tc-hib/go-winres` — внешний модуль. Умеет ли он MESSAGETABLE, **не проверял**. По PLAN §12 п. 51 генератор в go.mod не добавляется;
  - rc.exe и mc.exe в CI нет.
- Закоммиченные `.syso` собраны для `0.0.0-dev`, поэтому `go build ./...` и тесты получают ресурсы. Релизная сборка перегенерирует их с `$V` в рабочей копии job (без коммита). Тест следит, что закоммиченный `.syso` совпадает с выводом генератора для `0.0.0-dev` (как устаревание `.syso` в `TestCHK_TRAY_10_Manifest`).
- Один `.syso` на бинарник. Сливает ли линкер Go 1.20 несколько `.rsrc` из разных `.syso`, **не проверял**, поэтому на это не полагаемся. Имя `*_windows_amd64.syso` исключает файл из linux-сборок.

### 8.3. Манифесты

| Exe | requestedExecutionLevel | Common Controls v6 | supportedOS |
|---|---|---|---|
| ExchangeCheckerSetup.exe | **requireAdministrator** | да (кнопки, прогресс, SHBrowseForFolder) | Win7, 8, 8.1, 10/11 (те же GUID, что в `exchtray.manifest`) |
| ExchangeCheckerSvc.exe | asInvoker | не нужен | Win7+ |
| ExchangeCheckerTray.exe | asInvoker (уже есть) | да (уже есть) | Win7+ (уже есть) |

- `dpiAware` не добавлять: у трея его нет, держим одинаково (п. 21).
- Значок setup: переиспользовать `cmd/exchtray/exchtray.ico` или нарисовать свой (п. 5).
- VERSIONINFO: CompanyName (= Publisher, **не задан**), FileDescription, FileVersion, InternalName, LegalCopyright (**не задан**), OriginalFilename, ProductName `Exchange Checker`, ProductVersion `$V`. Translation — `040904B0` (EN, Unicode), блок RU по желанию.

---

## 9. Упаковка (CHK-DELIV-01) и проверка в CI без артефактов

- Правило CHECKS.md и `ci.yml` («No build artifacts are uploaded», CHK-CI-01) **запрещает только загрузку**. Сборка и проверка zip внутри job ему не противоречат, поэтому для проверки в CI **правило менять не нужно**.
- Новые шаги CI:
  1. **windows и linux, «release-like build»**: `$V=0.0.0-ci.$GITHUB_RUN_NUMBER`, порядок из §1.1 в `$RUNNER_TEMP/out`.
  2. **zip**: `go run ./tools/mkzip -o $RUNNER_TEMP/out/ExchangeChecker-$V-windows-amd64.zip $RUNNER_TEMP/out/ExchangeCheckerSetup.exe`. `archive/zip`, фиксированные дата и атрибуты для воспроизводимости; утилиты `zip` в Git Bash на Windows может не быть. Тот же инструмент пойдёт в фазу 8.
  3. **Проверка**: `EC_BUILD_DIR=$RUNNER_TEMP/out go test ./checks -run 'CHK_DELIV_01|CHK_RES_01|CHK_VER_01' -count=1 -v`. Без переменной тесты пропускаются (как `EC_SQL_TEST_*`). Что проверяется:
     - в zip ровно одна запись `ExchangeCheckerSetup.exe`, нет `go.mod`/`go.sum`, `.go`, `node_modules`, `package.json`, тестов;
     - запись — PE amd64 с подсистемой GUI;
     - VERSIONINFO `ProductVersion == $V`;
     - манифест `requireAdministrator`;
     - вложенные svc и tray найдены в образе (по сигнатуре `MZ…PE` в rodata или по известному смещению символа), их VERSIONINFO `== $V`;
     - у svc есть MESSAGETABLE с ID из `alert.EventIDs()`.
  4. **Только windows, E2E (CHK-SETUP-12)** — отдельный шаг **после** `go test ./...`. Установленная служба создаёт `\\.\pipe\ExchangeChecker` с `FILE_FLAG_FIRST_PIPE_INSTANCE` и сломала бы pipe-тесты CHK-MON-06, поэтому этот шаг отделён и гейтится `EC_SETUP_E2E=1`. Сценарий: тихая установка → проверка → установка версии V2 поверх → проверка сохранности данных → тихое удаление → проверка удаления.
  5. Шаг CI «setup dependencies (CHK-SETUP-11)» по образцу «tray dependencies (CHK-TRAY-10)», плюс `go vet -tags payload ./cmd/exchsetup` после сборки payload.
- **Отдельный пункт, нужно решение (п. 7): канал поставки zip заказчику (фаза 8).** Сейчас артефакты запрещены (CHK-CI-01, PLAN §11 фаза 1). Варианты:
  - (а) правило не трогать; zip собирается по явной просьбе в среде нейросети (кросс-компиляция с linux, Go 1.20.14) и передаётся в чат;
  - (б) GitHub Release asset на тег `vX.Y.Z` в отдельном workflow `release.yml` (`contents: write`, `workflow_dispatch` / tag). Нужно поправить CHK-CI-01 и PLAN;
  - (в) Actions artifact с коротким сроком хранения, только в ручном `release.yml`. Нужно поправить CHK-CI-01 в этой части.
  - Рекомендую (б) или (а). Решает координатор или заказчик.

---

## 10. Новые check ID, тесты и подфазы

### 10.1. Реестр проверок

| Check ID | Что проверяется | Автотест | Где | Подфаза |
|---|---|---|---|---|
| `CHK-RES-01` | Генератор `internal/winres`: корректный COFF (`debug/pe` открывает `.syso`, число релокаций = числу data entry), ресурсы ICON/GROUP_ICON/MANIFEST/VERSION/MESSAGETABLE проходят цикл запись → разбор; закоммиченные `.syso` совпадают с выводом генератора для `0.0.0-dev`; в собранных exe есть ожидаемые ресурсы | `TestCHK_RES_01_*` (`internal/winres`, `checks`) | linux + windows; exe — шаг CI | 7a |
| `CHK-VER-01` | `var Version` переопределяется через `-X`; отображение semver → FILEVERSION; VERSIONINFO всех трёх exe `== $V`; DisplayVersion = Version | `TestCHK_VER_01_*` + шаг CI | linux + windows | 7a |
| `CHK-SVC-02` (EC Service) | MESSAGETABLE в ExchangeCheckerSvc.exe: все `alert.EventIDs()` есть, ID без старших битов, текст `%1`; `FormatMessageW` из своего модуля возвращает вставку | `TestCHK_SVC_02_MessageTable` (`cmd/exchsvc`) | windows (FormatMessage), linux (разбор) | 7a |
| `CHK-SETUP-11` | Политика сборки setup: в графе `cmd/exchsetup` (windows/amd64) нет `os/exec`, `database/sql`, `go-mssqldb`, `internal/sqlcheck`, `golang.org/x/sys`; манифест requireAdministrator + comctl6 + supportedOS Win7+; каждый `NewProc` в allowlist ≤ 6.1 (общий список CHK-TRAY-10); vtable-исключение для COM задокументировано | `TestCHK_SETUP_11_*` (`checks`) + шаг CI | linux + windows | 7a (граф, манифест), дополняется в 7b–7d |
| `CHK-SETUP-01` (есть, planned) | Логика без побочных эффектов: разбор командной строки, проверка пути, планы шагов для свежей установки, обновления, удаления и purge (порядок: служба остановлена до удаления Run; Uninstall-ключ последний), значения Uninstall-ключа | `TestCHK_SETUP_01_*` (`internal/wininstall`, фейковый Executor, `-race`) | linux + windows | 7b |
| `CHK-SETUP-02` | Payload: тег `payload`; без него установка отказывается; запись + проверка sha256; атомарная замена с повтором; версия payload = версия setup | `TestCHK_SETUP_02_*` + шаг CI | linux + windows | 7b |
| `CHK-SETUP-03` | SCM: параметры CreateServiceW (own process, ImagePath в кавычках, LocalSystem), описание, failure actions + флаг, delayed, DACL по SDDL §2.3; размеры структур amd64; windows — тестовая служба `ExchangeCheckerTest-<rand>` (не запускается) → `QueryServiceConfigW`/`QueryServiceConfig2W`/`QueryServiceObjectSecurity`; токен с deny-only BA → QUERY_STATUS разрешён, START запрещён; `DeleteService` в Cleanup | `TestCHK_SETUP_03_*` | linux (фейк, размеры), windows (SCM) | 7b |
| `CHK-SVC-01` (есть, planned) | Настоящая служба под SCM: собранный `ExchangeCheckerSvc.exe` (`EC_SVC_EXE`) регистрируется под тестовым именем с `-config <tmp>` и `settings.json {"trayAutostart":false}`, чтобы реальный HKLM Run не получил значение; Start → RUNNING ≤ 60 с, канал отвечает (`ipc.DialPipe`, `hello`), Stop → STOPPED ≤ 90 с, Delete | `TestCHK_SVC_01_RealSCM` (гейт `EC_SVC_E2E=1`) | windows, отдельный шаг CI после `go test` | 7b |
| `CHK-SETUP-04` | Источник Event Log: `EventMessageFile` REG_EXPAND_SZ = путь svc, `TypesSupported` = 7, удаление; windows — тестовый источник `ExchangeCheckerTest-<rand>` → `ReportEventW` → `FormatMessageW` из EventMessageFile возвращает вставку | `TestCHK_SETUP_04_*` | linux (план), windows (реестр) | 7b |
| `CHK-SETUP-05` | ACL: ProgramData `D:PAI…` защищён и применён; `secrets.dpapi` остаётся SY+BA protected; существующие файлы получают наследуемые ACE; новые файлы наследуют; повтор идемпотентен; владелец BA; папка установки защищена (Users не пишут) | `TestCHK_SETUP_05_*` (windows, временные каталоги, `winapi.FileDACL`) | windows; SDDL-константы — linux | 7b |
| `CHK-SETUP-06` | HKLM Run: строка = `config.TrayRunCommand(путь svc)`; свежая установка → записано; `trayAutostart:false` → не записано или удалено; нечитаемые настройки → не тронуто; деинсталляция → удалено | `TestCHK_SETUP_06_*` (фейковый `config.RunRegistry`; windows — тестовый ключ, как `_RegistryRunWindows`) | linux + windows | 7b |
| `CHK-SETUP-07` | Uninstall-ключ (значения и типы, QuietUninstallString), деинсталлятор: шаги, purge против сохранения данных, самоудаление (вынос в `%SystemRoot%\Temp` + отложенное удаление / код 3010) | `TestCHK_SETUP_07_*` (фейк; windows — реестр на тестовом пути, rename запущенного exe на временном файле) | linux + windows | 7b |
| `CHK-SETUP-09` | Обновление: обнаружение (Uninstall-ключ / ImagePath), остановка службы с таймаутом (фейковые часы), остановка трея (фейковый список процессов: свой сеанс → сообщение, чужой → Terminate), замена с повтором и откатом, ProgramData не тронута (хэши), тип запуска сохранён, служба перезапускается только если работала, даунгрейд → код 3, та же версия → переустановка | `TestCHK_SETUP_09_*` | linux + windows | 7c |
| `CHK-TRAY-11` (EC Tray) | Трей и окно `--settings` завершаются по зарегистрированному сообщению `ExchangeChecker.TrayExit` без вопросов, значок удаляется (`App.Exit`) | `TestCHK_TRAY_11_*` (`internal/wintray`, скрытые окна) | windows | 7c |
| `CHK-SETUP-08` | Ярлык через COM `IShellLinkW`/`IPersistFile`: GUID, индексы vtable; создание `.lnk` во временной папке и чтение назад (GetPath, GetArguments, GetIconLocation); удаление | `TestCHK_SETUP_08_*` | windows | 7d |
| `CHK-SETUP-10` | Мастер UI: навигация по страницам через `SendMessage` (Далее/Назад), ошибки проверки пути, значения чекбоксов по умолчанию, «Готово» → флаг запуска; в тихом режиме окон нет; каталог строк RU/EN полон | `TestCHK_SETUP_10_*` | windows (UI), linux + windows (каталог) | 7d |
| `CHK-DELIV-01` (есть, planned) | Zip: одна запись `ExchangeCheckerSetup.exe`, детерминированный, без исходников и артефактов разработки; PE amd64 GUI, VERSIONINFO `$V`; собирается и проверяется в CI **без загрузки** | `TestCHK_DELIV_01_*` (`checks`, `EC_BUILD_DIR`) | linux + windows (шаг CI) | 7e |
| `CHK-SETUP-12` | E2E на windows-раннере: `/S /DIR=` → служба (конфигурация, DACL, delayed, failure actions), ключ Event Log, Run, Uninstall-ключ, файлы, ACL, ярлык → RUNNING → установка V2 поверх (хэши ProgramData не меняются) → `/uninstall /S` → всё удалено (данные остаются без `/PURGEDATA`) → `/uninstall /S /PURGEDATA` на втором прогоне | `TestCHK_SETUP_12_E2E` (гейт `EC_SETUP_E2E=1`; Setup запускается через `os.StartProcess` в тесте или из шага CI) | windows, отдельный шаг после `go test` | 7b (базовый), 7c (обновление), 7e (финал) |

Что проверяется только на Windows-раннере (`windows-latest` в `ci.yml` есть, процесс на нём с правами администратора): SCM, реестр HKLM, DACL, FormatMessage, COM-ярлык, UI-окна, E2E. Всё остальное (планы, разбор, генератор и разбор ресурсов, zip, граф зависимостей) работает на linux через кросс-компиляцию и unit-тесты. **Win7 в CI не проверяется** (PLAN §12 п. 56): ручная приёмка.

### 10.2. Подфазы (каждая — отдельный зелёный CI)

**7a — версия и ресурсы (инфраструктура). ✅ реализовано (ветка `phase7a`).**
- Сделать: `var Version`; `internal/winres` + `tools/mkres`; `cmd/exchsetup/rsrc_windows_amd64.syso` + `exchsetup.manifest` (requireAdministrator); константы `config.ServiceExeName`/`SetupExeName`; шаги CI «release-like build» (svc, tray, setup **пока без payload**) и статическая проверка ресурсов; правка `cmd/exchtray/RESOURCES.md`, если трей переходит на mkres.
- Проверки: CHK-RES-01, CHK-VER-01, CHK-SETUP-11 (граф и манифест) — EC Setup; CHK-SVC-02 — EC Service.
- Сделано EC Setup (решение 10): `internal/winres`, `tools/mkres`, `.syso` и манифесты setup и службы (S2), S5.
- **От EC Service:** S1 (`alert.EventIDs()`), S3 (тест CHK-SVC-02), S4 (контракт ReportEventW).
- T3 (трей на mkres) — в 7e.
- Зависимости: нет.

**7b — ядро установки и удаления без мастера (тихий режим). ✅ реализовано (ветка `phase7b`).**
- Сделать: `internal/wininstall` (план, исполнитель), payload (`-tags payload`), файлы, DACL папки установки, ACL ProgramData, SCM (create, config2, DACL, start, stop, delete), источник Event Log, HKLM Run, Uninstall-ключ, копия-деинсталлятор и самоудаление, разбор CLI (`/S /DIR= /NOSTART /LOG= /uninstall /PURGEDATA`), журнал; новые обёртки в `internal/winapi` + allowlist; шаг CI сборки с payload; гейтированный базовый E2E (установка и удаление).
- Проверки: CHK-SETUP-01..07, CHK-SVC-01, базовый CHK-SETUP-12.
- **От EC Service:** только то, что сделано в 7a (FormatMessage-проверка CHK-SETUP-04 опирается на MESSAGETABLE в svc). Изменения кода службы не нужны.
- Зависимости: 7a.

**7c — обновление поверх.**
- Сделать: обнаружение, остановка службы, остановка трея, замена с откатом, политика даунгрейда, сохранение типа запуска, E2E-обновление (сборки V1 и V2 в job).
- Проверки: CHK-SETUP-09, CHK-TRAY-11, CHK-SETUP-12 (обновление).
- **От EC Tray (обязательно):** T1 — `RegisterWindowMessageW("ExchangeChecker.TrayExit")`, обработка в `App.wndProc` → `App.Exit()` и в окнах `--settings`/Discovery → немедленный выход без вопроса о несохранённых правках; заодно `WM_CLOSE` главного окна → `App.Exit()`, чтобы убирать значок. Имя сообщения — экспортированная константа (например, `wintray.ExitMessage` или в `internal/model`), чтобы setup не импортировал wintray.
- **От EC Tray (по желанию, если координатор выберет):** T2 — межсеансовое событие `Global\ExchangeCheckerTrayExit`.
- Зависимости: 7b, T1.

**7d — мастер UI, ярлыки, локализация.**
- Сделать: страницы мастера, `SHBrowseForFolderW`, прогресс, чекбокс на «Готово», диалог удаления с чекбоксом данных, COM-ярлык, каталог RU/EN; по решению п. 8 — запуск трея без повышения прав.
- Проверки: CHK-SETUP-08, CHK-SETUP-10.
- Зависимости: 7b. Можно параллельно с 7c.

**7e — упаковка и финал.**
- Сделать: `tools/mkzip`, CHK-DELIV-01 в CI (без загрузки), полный E2E, документация: DELIVERY.md (состав папки установки, деинсталлятор, коды выхода, ключи CLI, таблица событий), PLAN §9/§11 (фаза 7 ✅, решения в §12), CHECKS.md (статусы), README (статус).
- Проверки: CHK-DELIV-01, CHK-SETUP-12 (финал).
- Зависимости: 7c, 7d. Канал поставки (п. 7) — решение до фазы 8.

### 10.3. Сводка: что нужно от EC Service и EC Tray

| # | Команда | Что | Подфаза |
|---|---|---|---|
| S1 | EC Service | `alert.EventIDs()`: единый список ID событий | 7a |
| S2 | EC Setup (решение 10), ✅ 7a | `cmd/exchsvc/rsrc_windows_amd64.syso`: MESSAGETABLE (`%1`, LANG_NEUTRAL, блоки 100–105, 110), VERSIONINFO, манифест asInvoker + supportedOS; исходники и заметка о пересборке | 7a |
| S3 | EC Service | `TestCHK_SVC_02_MessageTable` | 7a |
| S4 | EC Service | Зафиксировать контракт `ReportEventW` (категория 0, одна вставка, «голый» ID) | 7a |
| S5 | EC Setup (решение 10), ✅ 7a | `config.ServiceExeName`, `SetupExeName`, `UninstallExeName` | 7a |
| T1 | EC Tray | Выход по `ExchangeChecker.TrayExit` (главное окно, `--settings`, Discovery) без вопросов + `WM_CLOSE` → `App.Exit()`; тест CHK-TRAY-11 | 7c |
| T2 | — | Не делаем (решение 9: `TerminateProcess` в чужих сеансах) | — |
| T3 | EC Tray | `.syso` трея через mkres с VERSIONINFO, правка RESOURCES.md и CHK-TRAY-10 | 7e |

---

## 11. Риски и открытые вопросы (закрыты решениями координатора §D; номера совпадают)

1. **Чекбокс службы.** POLICY §2 говорит о «галочке автозапуска на «Готово»», PLAN §9 — о «Запустить службу после установки». Предложение: один чекбокс «Запустить службу и запускать автоматически»; выключен — DEMAND_START, служба не запускается. Альтернатива — два чекбокса.
2. **Отложенный автозапуск (delayed):** рекомендую да. Минус — старт примерно через 2 минуты после загрузки.
3. **Failure actions:** перезапуск через 60, 120, 300 с, сброс счётчика через сутки — подтвердить.
4. **Чтение для Users в ProgramData** (PLAN §12 п. 9, DELIVERY). Трей файлы больше не читает, а `profiles.json` открывает `manualSql` и mapping, которые IPC даёт только администратору. Оставить BU-read или перейти на SY+BA (и поправить п. 9 и DELIVERY)? Логи (`logs\exchsvc.log`) сейчас тоже читают Users.
5. **Publisher / CompanyName / LegalCopyright / значок setup** в репозитории не заданы.
6. **Версионирование:** источник (`vX.Y.Z`-тег), формат CI-версий, политика даунгрейда (отказ + `/ALLOWDOWNGRADE`), как обращаться с pre-release.
7. **Канал поставки zip** при запрете артефактов (CHK-CI-01): среда нейросети, GitHub Release или ручной artifact-workflow.
8. **Запуск трея после установки или обновления.** Из повышенного setup трей запустился бы с повышенными правами, этого не хотим. Варианты:
   - не запускать (значок появится при следующем входе);
   - запустить без повышения в своём сеансе через токен explorer.exe (`OpenProcessToken` + `DuplicateTokenEx` + `CreateProcessWithTokenW`, 6.0). Это создание процесса продукта, не helper-процесс, но формально это решение по POLICY §4.
9. **Остановка трея в чужих сеансах:** `TerminateProcess` (значок-«призрак» до наведения мыши) или доработка T2. `TerminateProcess` для зависшей службы — только с согласия пользователя.
10. **Кто владеет генератором `internal/winres`/`tools/mkres`** (EC Setup?) и переводится ли трей с akavel/rsrc (зона EC Tray, PLAN §12 п. 51 говорит «генератор в go.mod не добавляется» — tools/mkres в go.mod ничего не добавляет).
11. **Деинсталлятор:** копия полного setup (C1, размер) или маленькая сборка без payload (C2); имя файла; 4-й файл в папке установки нужно отразить в DELIVERY.md.
12. **Самоудаление** оставляет файл до перезагрузки (в `%SystemRoot%\Temp` или в папке установки, код 3010). Допустимо?
13. **E2E на раннере трогает настоящий HKLM** (службы, Uninstall, EventLog; Run — только удаление отсутствующего значения при `trayAutostart:false`). В CHECKS для CHK-CFG-04 записано «реальный HKLM Run тесты не трогают». Нужно явное разрешение для гейтированных E2E-шагов на одноразовой ВМ.
14. **Win7 не проверяется в CI.** Ручная приёмка: `SHBrowseForFolderW`, `IShellLinkW`, уровни `ChangeServiceConfig2W` 3 и 4, comctl v6, `MOVEFILE_DELAY_UNTIL_REBOOT`, delayed start. 32-битная Win7 не поддерживается (amd64-only).
15. **Учётная запись службы** — только LocalSystem. Windows-аутентификация к SQL идёт от `DOMAIN\HOST$`. Выбор учётной записи в мастере в v1 не делаем (подтвердить).
16. **Захват `%ProgramData%\ExchangeChecker` до установки** обычным пользователем: смена владельца на BA рекурсивно? Или отказ установки, если владелец чужой?
17. **ACL папки установки** вне Program Files: защищённый DACL ставим всегда (подтвердить).
18. **Нет подписи кода:** неподписанный Go-инсталлятор с вложенными exe может давать ложные срабатывания антивирусов и предупреждение SmartScreen. Сертификата в проекте нет.
19. **Размер Setup.exe** не измерен (сборки в этой задаче запрещены). Решение о сжатии payload — после 7b.
20. **Язык описания службы и имён ярлыков:** язык установки или оба? Нужен ли ярлык на «Настройки» (`--settings`)?
21. **dpiAware в манифесте setup:** оставить без него, как у трея?
22. **Тег `payload`:** обычные `go vet ./...` и `go build ./...` его не видят, поэтому нужен отдельный `go vet -tags payload` после сборки payload (CI-шаг в 7b).
23. **При удалении ключа источника Event Log** старые события показываются с «описание не найдено». Стандартное поведение; удалять ли ключ, если данные сохранены, — решение (рекомендую удалять).
24. **HKCU других пользователей** (`Software\ExchangeChecker\Tray`) при удалении остаются.
25. **Параллельные Go-тесты.** `go test ./...` прогоняет пакеты параллельно, а имя канала фиксировано (`model.PipeName`). Все тесты с настоящей службой (CHK-SVC-01, CHK-SETUP-12) — только в отдельных гейтированных шагах.

---

## Приложение A. Новые WinAPI-функции для allowlist (`checks/tray10_test.go`)

| DLL | Функция | Минимальная версия NT | Подфаза |
|---|---|---|---|
| advapi32 | CreateServiceW, ChangeServiceConfigW, ChangeServiceConfig2W (уровни 3 и 4 требуют 6.0), QueryServiceConfigW, QueryServiceConfig2W, QueryServiceObjectSecurity, SetServiceObjectSecurity, StartServiceW, ControlService, DeleteService | 5.0 | 7b |
| advapi32 | OpenProcessToken, DuplicateTokenEx | 5.0 | 7d (если п. 8 = запуск трея) |
| advapi32 | CreateProcessWithTokenW | 6.0 | 7d (если п. 8) |
| kernel32 | CreateToolhelp32Snapshot, Process32FirstW, Process32NextW, OpenProcess, TerminateProcess, WaitForSingleObject, ProcessIdToSessionId, GetDriveTypeW, GetDiskFreeSpaceExW | 5.0 | 7b/7c |
| kernel32 | QueryFullProcessImageNameW | 6.0 | 7c |
| user32 | FindWindowExW, GetWindowThreadProcessId (остальное для UI уже в списке) | 5.0 | 7c/7d |
| shell32 | SHBrowseForFolderW, SHGetPathFromIDListW, SHGetFolderPathW, SHChangeNotify | 5.0 | 7b/7d |
| ole32 | OleInitialize, CoInitializeEx, CoUninitialize, CoCreateInstance, CoTaskMemFree | 5.0 | 7d |
| comctl32 | InitCommonControlsEx | 5.0 | 7d |

`FormatMessageW`, `LoadLibraryExW` и `FreeLibrary` нужны только в `_test.go`; allowlist их не сканирует (`goSources` пропускает `_test.go`). Уже есть в списке и переиспользуются: `RegCreateKeyExW`, `RegSetValueExW`, `RegDeleteValueW`, `RegDeleteKeyExW` (6.0), `SetNamedSecurityInfoW`, `GetNamedSecurityInfoW`, `ConvertStringSecurityDescriptorToSecurityDescriptorW`, `OpenSCManagerW`, `OpenServiceW`, `QueryServiceStatusEx`, `CloseServiceHandle`, `MoveFileExW`, `CheckTokenMembership`, `RegisterWindowMessageW`, `PostMessageW`, `CreateWindowExW`, `MessageBoxW`, `ShellExecuteExW`, `GetModuleFileNameW`.
