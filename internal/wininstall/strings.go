package wininstall

import "fmt"

// Setup string catalog RU/EN (docs/PHASE7.md §1.3, CHK-SETUP-10): keys
// setup.*; completeness is tested (TestCHK_SETUP_10_Catalog*). The tray
// catalog internal/i18n is EC Tray's and is not used here.

var catalog = map[string]map[string]string{
	"ru": {
		"setup.title":             "Установка Exchange Checker %s",
		"setup.uninstall.title":   "Удаление Exchange Checker",
		"setup.btn.back":          "< Назад",
		"setup.btn.next":          "Далее >",
		"setup.btn.install":       "Установить",
		"setup.btn.cancel":        "Отмена",
		"setup.btn.finish":        "Готово",
		"setup.btn.browse":        "Обзор…",
		"setup.btn.uninstall":     "Удалить",
		"setup.brand.tagline":     "Мониторинг обменов 1С",
		"setup.step.welcome":      "Приветствие",
		"setup.step.folder":       "Папка",
		"setup.step.options":      "Опции",
		"setup.step.progress":     "Установка",
		"setup.step.finish":       "Готово",
		"setup.welcome.fresh":     "Мастер установит Exchange Checker %s на этот компьютер: службу контроля обменов 1С и значок в области уведомлений.\r\n\r\nНажмите «Далее», чтобы продолжить.",
		"setup.welcome.upgrade":   "Установлена версия %s, будет обновлена до %s. Настройки в %%ProgramData%%\\ExchangeChecker сохранятся.\r\n\r\nНажмите «Далее», чтобы продолжить.",
		"setup.welcome.reinstall": "Версия %s уже установлена и будет переустановлена. Настройки сохранятся.\r\n\r\nНажмите «Далее», чтобы продолжить.",
		"setup.welcome.downgrade": "Установлена более новая версия %s. Установка версии %s отменена: понижение версии запрещено (только с ключом /ALLOWDOWNGRADE).\r\n\r\nНажмите «Отмена», чтобы выйти.",
		"setup.folder.browsefail": "Не удалось получить путь выбранной папки: %s",
		"setup.folder.label":      "Папка установки:",
		"setup.folder.upgrade":    "При обновлении папка не меняется.",
		"setup.folder.browse":     "Выберите папку установки Exchange Checker",
		"setup.options.shortcut":  "Ярлык в меню «Пуск»",
		"setup.options.service":   "Запустить службу и запускать автоматически",
		"setup.progress.label":    "Установка…",
		"setup.finish.ok":         "Exchange Checker %s установлен.",
		"setup.finish.reboot":     "Exchange Checker %s установлен. Для завершения нужна перезагрузка.",
		"setup.finish.error":      "Установка не выполнена: %s\r\n\r\nЖурнал установки: %s",
		"setup.finish.tray":       "Запустить Exchange Checker (значок в области уведомлений)",
		"setup.finish.trayfail":   "Значок появится при следующем входе в систему.",
		"setup.cancel.confirm":    "Прервать установку?",
		"setup.uninstall.text":    "Exchange Checker будет удалён с этого компьютера: служба, значок и ярлыки.",
		"setup.uninstall.purge":   "Удалить настройки и пароли (%ProgramData%\\ExchangeChecker)",
		"setup.uninstall.done":    "Exchange Checker удалён.",
		"setup.uninstall.reboot":  "Exchange Checker удалён. Оставшиеся файлы будут удалены после перезагрузки.",
		"setup.uninstall.error":   "Удаление выполнено не полностью: %s\r\n\r\nЖурнал: %s",
		"setup.shortcut.desc":     "Exchange Checker — контроль обменов 1С",
		"setup.err.admin":         "Нужны права администратора.",
		"setup.err.running":       "Установка уже запущена.",
	},
	"en": {
		"setup.title":             "Exchange Checker %s Setup",
		"setup.uninstall.title":   "Uninstall Exchange Checker",
		"setup.btn.back":          "< Back",
		"setup.btn.next":          "Next >",
		"setup.btn.install":       "Install",
		"setup.btn.cancel":        "Cancel",
		"setup.btn.finish":        "Finish",
		"setup.btn.browse":        "Browse…",
		"setup.btn.uninstall":     "Uninstall",
		"setup.brand.tagline":     "1C exchange monitoring",
		"setup.step.welcome":      "Welcome",
		"setup.step.folder":       "Folder",
		"setup.step.options":      "Options",
		"setup.step.progress":     "Installing",
		"setup.step.finish":       "Finish",
		"setup.welcome.fresh":     "This wizard installs Exchange Checker %s on this computer: the 1C exchange monitoring service and the notification-area icon.\r\n\r\nClick Next to continue.",
		"setup.welcome.upgrade":   "Version %s is installed and will be upgraded to %s. Settings in %%ProgramData%%\\ExchangeChecker are kept.\r\n\r\nClick Next to continue.",
		"setup.welcome.reinstall": "Version %s is already installed and will be reinstalled. Settings are kept.\r\n\r\nClick Next to continue.",
		"setup.welcome.downgrade": "A newer version %s is installed. Installing %s is cancelled: downgrade is not allowed (only with /ALLOWDOWNGRADE).\r\n\r\nClick Cancel to exit.",
		"setup.folder.browsefail": "Could not read the path of the chosen folder: %s",
		"setup.folder.label":      "Installation folder:",
		"setup.folder.upgrade":    "The folder does not change on upgrade.",
		"setup.folder.browse":     "Choose the Exchange Checker installation folder",
		"setup.options.shortcut":  "Start menu shortcut",
		"setup.options.service":   "Start the service and start it automatically",
		"setup.progress.label":    "Installing…",
		"setup.finish.ok":         "Exchange Checker %s has been installed.",
		"setup.finish.reboot":     "Exchange Checker %s has been installed. A restart is required to finish.",
		"setup.finish.error":      "Setup failed: %s\r\n\r\nSetup log: %s",
		"setup.finish.tray":       "Launch Exchange Checker (notification-area icon)",
		"setup.finish.trayfail":   "The icon will appear at the next sign-in.",
		"setup.cancel.confirm":    "Cancel the installation?",
		"setup.uninstall.text":    "Exchange Checker will be removed from this computer: the service, the icon and the shortcuts.",
		"setup.uninstall.purge":   "Remove settings and passwords (%ProgramData%\\ExchangeChecker)",
		"setup.uninstall.done":    "Exchange Checker has been removed.",
		"setup.uninstall.reboot":  "Exchange Checker has been removed. Remaining files are deleted after a restart.",
		"setup.uninstall.error":   "Uninstall did not complete: %s\r\n\r\nLog: %s",
		"setup.shortcut.desc":     "Exchange Checker — 1C exchange monitoring",
		"setup.err.admin":         "Administrator rights are required.",
		"setup.err.running":       "Setup is already running.",
	},
}

// CatalogLangs are the setup languages.
var CatalogLangs = []string{"ru", "en"}

// NormLang maps any language to ru/en (fallback ru, PLAN §12 п. 8).
func NormLang(lang string) string {
	if lang == "en" {
		return "en"
	}
	return "ru"
}

// T returns the catalog string key in lang (ru fallback), formatted with
// args when given. A missing key returns the key itself.
func T(lang, key string, args ...interface{}) string {
	s, ok := catalog[NormLang(lang)][key]
	if !ok {
		if s, ok = catalog["ru"][key]; !ok {
			return key
		}
	}
	if len(args) > 0 {
		return fmt.Sprintf(s, args...)
	}
	return s
}

// CatalogKeys returns the keys of lang (tests).
func CatalogKeys(lang string) map[string]string { return catalog[lang] }
