# third_party/inpout — InpOutx64 kernel driver (highrez)

Фаза 14, решение Алекса 10.11.2026: встроенный сигнал «Тон» звучит через
системный динамик (PC speaker) на Win7 x64 с помощью подписанного драйвера
ядра InpOutx64. Исключение из правила POLICY «без драйверов» — см. POLICY.md.

Файл `inpoutx64.sys` лежит **без изменений**, байт в байт как в официальном
дистрибутиве. Пересобирать, патчить или переподписывать его нельзя.

| Поле | Значение |
|---|---|
| Источник (страница) | https://www.highrez.co.uk/downloads/inpout32/ |
| Архив | https://www.highrez.co.uk/downloads/InpOut32/InpOutInterfaceDriver_1501.zip (ссылка «InpOutInterfaceDriver» на странице, `scripts/download.asp?package=InpOutInterfaceDriver`) |
| sha256 архива | `35dd357767ea3a4a523fe31126eba1842572f2c060c5c1337d9659c2b0343997` (21 662 байт) |
| Путь в архиве | `inpoutx64.sys` (корень архива, 15 008 байт, 2008-10-18) |
| sha256 `.sys` | `f8965fdce668692c3785afa3559159f9a18287bc0d53abb21902895a8ecf221b` = `config.InpOutSHA256` |
| Версия | дистрибутив 1.5.0.1; FileVersion `1.2 x64 built by: WinDDK`, ProductName `inpoutx64 Driver Version 1.2` |
| Подписант | `CN=Red Fox UK Limited` (VeriSign Class 3 Code Signing 2004 CA → VeriSign Class 3 Public Primary CA, кросс-сертификат на Microsoft Code Verification Root) |
| Подпись | Authenticode SHA1, таймштамп VeriSign 2008-10-18 00:17:06 UTC (сертификат действителен 2008-10-09…2009-10-09) |
| Устройство | `\Device\inpoutx64`, `\DosDevices\inpoutx64` → `\\.\inpoutx64` (`hwinterfacedrv.c` из того же архива) |
| Лицензия | MIT, `LICENSE.txt` (копия `license.txt` из архива, совпадает с `license.txt` из `InpOutBinaries_1501.zip`) |

Тот же архив бинарников: https://www.highrez.co.uk/downloads/InpOut32/InpOutBinaries_1501.zip,
sha256 `7b56d19f0b18e5432a6564a5928ce53439dd5a632bcec1bb52a117be640b533f`.

## Риск

Драйвер есть в списке уязвимых драйверов loldrivers.io
(https://www.loldrivers.io/drivers/91ff1575-9ff2-46fd-8bfe-0bb3e3457b7f/,
этот sha256 указан), а блок-лист Microsoft
(https://aka.ms/VulnerableDriverBlockList) блокирует другие сборки InpOutx64
(`ID_DENY_INPOUTX_*`). Устройство создаётся без ограничивающего DACL и даёт
доступ к портам и физической памяти — локальное повышение привилегий, пока
драйвер загружен. Риск принят Алексом (решение 10.11.2026).

Проверки: CHK-SETUP-19 (хеш, подписант, подпись), CHK-SETUP-20 (установка,
удаление, откат).
