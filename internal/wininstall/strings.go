package wininstall

import "fmt"

// Setup string catalog, Russian only (phase 13, CHK-TRAY-86; docs/PHASE7.md §1.3, CHK-SETUP-10): keys
// setup.*; completeness is tested (TestCHK_SETUP_10_Catalog*). The tray
// catalog internal/i18n is EC Tray's and is not used here.

var catalog = map[string]string{

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
	"setup.folder.label":      "Выбор папки установки",
	"setup.folder.upgrade":    "При обновлении папка установки не меняется: программа обновится в уже выбранном каталоге.",
	"setup.folder.browse":     "Выберите папку установки Exchange Checker",
	"setup.options.shortcut":  "Ярлык в меню «Пуск»",
	"setup.options.desktop":   "Создать ярлык на рабочем столе",
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
}

// T returns the catalog string key, formatted with
// args when given. A missing key returns the key itself.
func T(key string, args ...interface{}) string {
	s, ok := catalog[key]
	if !ok {
		return key
	}
	if len(args) > 0 {
		return fmt.Sprintf(s, args...)
	}
	return s
}

// CatalogKeys returns the catalog (tests).
func CatalogKeys() map[string]string { return catalog }
