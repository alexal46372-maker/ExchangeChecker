# Фаза 14 — установщик: драйвер InpOutx64 (сигнал «Тон» через системный динамик)

Решение Алекса 10.11.2026: встроенный сигнал «Тон» звучит через системный
динамик (PC speaker) на Win7 x64 с помощью подписанного драйвера ядра
InpOutx64 от highrez. Это исключение из правила POLICY «без драйверов»
(POLICY.md §8). Служба (EC Service, `feat/svc-speaker`) открывает
`\\.\inpoutx64` и сама запускает службу драйвера; установщик (EC Setup,
`feat/setup-speaker`) только кладёт файл и регистрирует службу.

## 1. Файл и общие константы

`third_party/inpout/inpoutx64.sys` — без изменений, из
`https://www.highrez.co.uk/downloads/InpOut32/InpOutInterfaceDriver_1501.zip`
(sha256 архива `35dd357767ea3a4a523fe31126eba1842572f2c060c5c1337d9659c2b0343997`,
путь в архиве `inpoutx64.sys`). Версия файла `1.2 x64`, лицензия MIT.

| Константа (`internal/config/install.go`) | Значение |
|---|---|
| `InpOutServiceName` | `ExchangeCheckerInpOut` |
| `InpOutDevicePath` | `\\.\inpoutx64` |
| `InpOutRelPath` | `driver\inpoutx64.sys` |
| `InpOutSHA256` | `f8965fdce668692c3785afa3559159f9a18287bc0d53abb21902895a8ecf221b` |

Подпись: Authenticode SHA1, подписант `Red Fox UK Limited` (VeriSign Class 3
Code Signing 2004 CA), цепочка через кросс-сертификат VeriSign Class 3 Public
Primary CA → Microsoft Code Verification Root (политика подписи драйверов ядра
x64 Vista/7), таймштамп VeriSign 2008-10-18 00:17:06 UTC внутри срока
сертификата (2008-10-09…2009-10-09). Проверки — `CHK-SETUP-19`.

## 2. Установка (шаг 4b, `internal/wininstall/driver.go`)

После записи exe и до регистрации основной службы:

1. В payload нет драйвера или sha256 ≠ `InpOutSHA256` → WARNING, шаг пропущен.
2. Служба драйвера загружена → `ControlService(STOP)` (файл загруженного
   драйвера заблокирован); не остановилась → WARNING, файл не трогаем.
3. `<папка>\driver\` (новая — в локальный журнал), `.bak` существующего файла,
   запись `inpoutx64.sys` (temp + MoveFileEx + проверка sha256).
4. Служба: нет → `CreateServiceW(SERVICE_KERNEL_DRIVER, SERVICE_DEMAND_START,
   SERVICE_ERROR_NORMAL, "\??\<папка>\driver\inpoutx64.sys")`; есть →
   `ChangeServiceConfigW` с теми же значениями (идемпотентно). DACL
   `DriverSDDL`: start/stop/change — только SY и BA. **Не запускается.**
5. Любой сбой шагов 2–4 → откат изменений шага на месте, WARNING по-русски,
   установка продолжается (код 0). Завершённый шаг добавляет свой журнал в
   общий undo: сбой после него (основная служба, Uninstall-ключ, …) удаляет
   службу драйвера и файл (свежая установка) или восстанавливает прежний
   файл / ImagePath (апгрейд). `.bak` драйвера удаляется после успеха.

## 3. Удаление

`ControlService(STOP)` (не запущена / нет службы — успех; не выгружается —
WARNING и перезагрузка), `DeleteService`, удаление файла (занят — при
перезагрузке), `driver\` — если пуста; затем остальная папка установки.

## 4. Fault-hooks (только `-tags e2efault`)

`EC_SETUP_FAIL_AT=InpOutDriver` — сбой шага драйвера после записи файла
(ожидается WARNING и exit 0, файла и службы нет);
`EC_SETUP_FAIL_AT=AfterInpOutDriver` — фатальная ошибка сразу после шага
(ожидается откат службы драйвера и файла вместе с остальным).

## 5. Риск (честно для заказчика)

- loldrivers.io: https://www.loldrivers.io/drivers/91ff1575-9ff2-46fd-8bfe-0bb3e3457b7f/
  — «vulnerable driver», Verified, MITRE T1068 (повышение привилегий); среди
  образцов указан именно этот sha256.
- Блок-лист Microsoft (https://aka.ms/VulnerableDriverBlockList,
  `DriverPolicy_Enforced.xml` 10.0.29545.0) блокирует другие сборки
  InpOutx64 (`ID_DENY_INPOUTX_11…23`); Authenticode-хешей этого файла
  (SHA1 `8E1F51761F21148F68AC925CC5F9E9C78F3D5EC4`, SHA256
  `D61CE5874ADB89B4E992DF8DF879B568D9C4136DF568718A768CD807D789A726`) и
  правил по подписанту Red Fox в списке нет. На Win7 блок-лист не действует.
- Драйвер создаёт устройство через `IoCreateDevice` без ограничивающего DACL
  и умеет отображать `\Device\PhysicalMemory`: пока он загружен, любой
  локальный процесс может читать/писать порты и физическую память.
  Рекомендация для EC Service: загружать драйвер только на время сигнала и
  выгружать после.

## 6. Служба (EC Service)

Как служба Exchange Checker пользуется драйвером (ветка `feat/svc-speaker`, `CHK-SVC-12`):

- Драйвер загружается **только на время тона** (`StartService` перед записью портов; установщик его не запускает).
- После тишины служба драйвера останавливается **не позже чем через 2 секунды** (`ControlService(STOP)`). При остановке / shutdown / suspend службы Exchange Checker — сразу.
- Перед **каждой** загрузкой sha256 файла из ImagePath (`QueryServiceConfigW`, без самостоятельного конструирования пути) сверяется с `config.InpOutSHA256`. Несовпадение — `speaker_status` reason `blocked`, драйвер не запускается.
- Если при старте службы Exchange Checker драйвер уже загружен (остаток после сбоя), он останавливается сразу, до первого тона.
