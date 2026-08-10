package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// runRFIAutomation выполняет полный сценарий выгрузки RFI.
// Возвращает true при успехе, false при любой ошибке (программа НЕ закрывается).
func runRFIAutomation() (ok bool) {
	cfg := appConfig

	profileDir := filepath.Join(os.TempDir(), "rca-rod-RFI")

	log.Println("🚀 Запуск автоматизации RCA (RFI)...")
	updateStatus("🚀 Запуск автоматизации RCA...", 2)

	log.Printf("📁 Папка профиля браузера: %s", profileDir)

	if err := os.RemoveAll(profileDir); err != nil {
		log.Printf("⚠️ Не удалось удалить старую папку профиля: %v", err)
	}

	cleanRodTempDirs()

	absCwd, _ := filepath.Abs(".")
	cleanOldScreenshots(absCwd)

	var page *rod.Page

	defer func() {
		if r := recover(); r != nil {
			log.Printf("❌ СКРИПТ ОСТАНОВИЛСЯ С ОШИБКОЙ: %v", r)
			updateStatusError(fmt.Sprintf("❌ Ошибка RFI: %v", r))
			saveErrorScreenshot(page, "crash")
			ok = false
		}
		if err := os.RemoveAll(profileDir); err == nil {
			log.Println("🧹 Папка профиля браузера удалена")
		}
	}()

	updateStatus("📖 Чтение конфигурации...", 6)
	log.Printf("📂 Сетевая папка для выгрузок RFI: %s", cfg.RFIPath)

	homeDir, _ := os.UserHomeDir()
	downloadDir := filepath.Join(homeDir, "Downloads")
	if _, err := os.Stat(downloadDir); os.IsNotExist(err) {
		downloadDir = filepath.Join(homeDir, "Загрузки")
	}
	log.Printf("📂 Отслеживаем папку загрузок: %s", downloadDir)

	browserPath := findBrowser()
	log.Printf("🌐 Используем браузер: %s", browserPath)

	updateStatus("🌐 Запуск браузера...", 12)
	l := launcher.New().
		Bin(browserPath).
		UserDataDir(profileDir).
		Set("download.default_directory", filepath.ToSlash(downloadDir)).
		Set("download.prompt_for_download", "false").
		Set("safebrowsing.enabled", "false").
		Headless(true).
		Devtools(false)

	url, err := l.Launch()
	if err != nil {
		updateStatusError("❌ RFI: не удалось запустить браузер")
		log.Printf("❌ Не удалось запустить браузер: %v", err)
		return false
	}

	browser := rod.New().ControlURL(url).MustConnect()
	globalBrowser = browser
	defer browser.MustClose()
	defer func() { globalBrowser = nil }()

	page = browser.MustPage()

	updateStatus("📐 Настройка экрана...", 16)
	page.MustSetViewport(1920, 1080, 1, false)
	time.Sleep(500 * time.Millisecond)

	updateStatus("🔑 Выполняется вход в систему...", 22)
	page.MustNavigate("https://rca.sgaz.pro/")
	page.MustWaitLoad()

	page.MustElement("#loginInput").MustWaitVisible().MustInput(cfg.Login)
	page.MustElement("#passInput").MustWaitVisible().MustInput(cfg.Password)

	if err := page.Keyboard.Press(input.Enter); err != nil {
		updateStatusError("❌ RFI: ошибка нажатия Enter после пароля")
		saveErrorScreenshot(page, "login_enter")
		log.Printf("❌ Ошибка нажатия Enter после пароля: %v", err)
		return false
	}

	time.Sleep(4 * time.Second)
	updateStatus("✅ Вход выполнен", 30)

	updateStatus("📂 Переход к задачам...", 35)
	page.MustNavigate("https://rca.sgaz.pro/faces/page/contracts/52931/build-tracker/tasks")

	filterBtn := page.MustElement("#gw9gh4_9").MustWaitVisible()

	updateStatus("🔽 Открытие фильтра...", 40)
	filterBtn.MustClick()

	filterInput := page.MustElement("#gihyoe-suggest-input").MustWaitVisible()
	time.Sleep(500 * time.Millisecond)

	updateStatus("⌨️ Применение фильтра 'км'...", 46)
	filterInput.MustClick()
	time.Sleep(300 * time.Millisecond)
	filterInput.MustInput("км")

	time.Sleep(2 * time.Second)

	sendKeyThroughCDP(page, "Enter", proto.InputDispatchKeyEventTypeKeyDown)
	time.Sleep(100 * time.Millisecond)
	sendKeyThroughCDP(page, "Enter", proto.InputDispatchKeyEventTypeKeyUp)

	time.Sleep(2 * time.Second)

	updateStatus("✅ Фильтр 'км' применён", 52)
	page.MustElement(`[id="cjtce8::ok"]`).MustWaitVisible().MustClick()

	time.Sleep(4 * time.Second)

	updateStatus("📋 Выбор типа заявки RFI...", 58)
	dropdownTarget := page.MustElement("#hyj3sh-dropdown-target").MustWaitVisible()
	dropdownTarget.MustScrollIntoView()
	time.Sleep(300 * time.Millisecond)
	dropdownTarget.MustClick()

	time.Sleep(4 * time.Second)

	rfiSelected := false

	rfiElement, err := page.ElementX("//html/body/div[5]/div[4]/span")
	if err == nil && rfiElement != nil {
		rfiElement.MustClick()
		rfiSelected = true
	}

	if !rfiSelected {
		elements, err := page.ElementsX("//span[contains(text(), 'RFI (строительный контроль)')]")
		if err == nil && len(elements) > 0 {
			elements[0].MustClick()
			rfiSelected = true
		}
	}

	if !rfiSelected {
		updateStatusError("⚠️ RFI: не удалось выбрать тип заявки")
		saveErrorScreenshot(page, "rfi_not_selected")
		log.Println("⚠️ Не удалось выбрать RFI!")
	} else {
		updateStatus("✅ Тип заявки RFI выбран", 62)
	}

	time.Sleep(1 * time.Second)

	updateStatus("📅 Очистка поля даты...", 66)
	dateEl := page.MustElement(`[id="C8zl006::content"]`).MustWaitVisible()
	dateEl.MustScrollIntoView()
	time.Sleep(300 * time.Millisecond)
	dateEl.MustClick()
	time.Sleep(500 * time.Millisecond)

	_, err = page.Eval(`() => {
		const input = document.querySelector('[id="C8zl006::content"]');
		if (input) {
			input.value = ' ';
			input.dispatchEvent(new Event('change', { bubbles: true }));
			return 'cleared';
		}
		return 'not found';
	}`)
	if err != nil {
		log.Printf("⚠️ Ошибка очистки даты: %v", err)
	} else {
		updateStatus("✅ Поле даты очищено", 69)
	}

	time.Sleep(500 * time.Millisecond)

	updateStatus("🔍 Ввод 'SMU' в поиск...", 72)
	searchEl := page.MustElement(`[id="l5x27s::content"]`).MustWaitVisible()
	searchEl.MustScrollIntoView()
	time.Sleep(300 * time.Millisecond)
	searchEl.MustClick()
	time.Sleep(300 * time.Millisecond)

	_, err = page.Eval(`() => {
		const input = document.querySelector('[id="l5x27s::content"]');
		if (input) {
			input.value = 'smu';
			input.dispatchEvent(new Event('change', { bubbles: true }));
			return 'typed';
		}
		return 'not found';
	}`)
	if err != nil {
		log.Printf("⚠️ Ошибка ввода SMU: %v", err)
	} else {
		updateStatus("✅ Поиск 'SMU' введён", 75)
	}

	time.Sleep(500 * time.Millisecond)

	sendKeyThroughCDP(page, "Enter", proto.InputDispatchKeyEventTypeKeyDown)
	time.Sleep(100 * time.Millisecond)
	sendKeyThroughCDP(page, "Enter", proto.InputDispatchKeyEventTypeKeyUp)

	time.Sleep(3 * time.Second)

	updateStatus("📥 Выгрузка в Excel запущена...", 80)
	page.MustElement("#C5xhyht").MustWaitVisible().MustClick()

	updateStatus("⏳ Ожидание скачивания файла (до 5 минут)...", 84)
	localFilePath := waitForDownload(downloadDir, "Инспекции на*.xlsx", 5*time.Minute)

	if localFilePath == "" {
		updateStatusError("❌ RFI: файл не скачался за 5 минут")
		saveErrorScreenshot(page, "download_timeout")
		log.Println("❌ Файл не был скачан в течение 5 минут!")
		return false
	}

	updateStatus("✅ Файл скачан: "+filepath.Base(localFilePath), 92)

	if _, err := os.Stat(cfg.RFIPath); os.IsNotExist(err) {
		updateStatusError("❌ RFI: сетевая папка недоступна")
		saveErrorScreenshot(page, "network_unavailable")
		log.Printf("❌ Сетевая папка недоступна: %v", err)
		return false
	}

	updateStatus("🧹 Очистка старых файлов в сетевой папке...", 95)
	cleanOldFiles(cfg.RFIPath, "Инспекции на*.xlsx")

	destFileName := filepath.Base(localFilePath)
	destFilePath := filepath.Join(cfg.RFIPath, destFileName)

	updateStatus("📋 Копирование файла в сеть...", 97)
	if err := copyFile(localFilePath, destFilePath); err != nil {
		updateStatusError("❌ RFI: ошибка копирования в сетевую папку")
		saveErrorScreenshot(page, "copy_error")
		log.Printf("❌ Ошибка копирования файла в сетевую папку: %v", err)
		return false
	}

	if err := os.Remove(localFilePath); err != nil {
		log.Printf("⚠️ Не удалось удалить локальный файл: %v", err)
	} else {
		log.Println("✅ Локальная копия удалена")
	}

	log.Println("🎉 АВТОМАТИЗАЦИЯ RFI ПОЛНОСТЬЮ ЗАВЕРШЕНА!")
	updateStatus("🎉 Выгрузка RFI завершена", 100)
	return true
}
