package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/launcher"
)

// === ВСПОМОГАТЕЛЬНЫЕ ФУНКЦИИ ИД ===

// findCSS пробует несколько CSS-селекторов и возвращает первый найденный элемент
func findCSS(page *rod.Page, per time.Duration, stepName string, selectors ...string) *rod.Element {
	for _, sel := range selectors {
		if el, err := page.Timeout(per).Element(sel); err == nil {
			return el
		}
	}
	saveErrorScreenshot(page, stepName)
	panic(fmt.Sprintf("Элемент не найден ни по одному из селекторов: %v", selectors))
}

// findX пробует несколько XPath-селекторов и возвращает первый найденный элемент
func findX(page *rod.Page, per time.Duration, stepName string, xpaths ...string) *rod.Element {
	for _, xp := range xpaths {
		if el, err := page.Timeout(per).ElementX(xp); err == nil {
			return el
		}
	}
	saveErrorScreenshot(page, stepName)
	panic(fmt.Sprintf("Элемент не найден ни по одному из XPath: %v", xpaths))
}

// clickAddButton нажимает кнопку "Добавить элемент выбранный из списка"
func clickAddButton(page *rod.Page, preferredID string, stepName string) {
	if el, err := page.Timeout(3 * time.Second).Element("#" + preferredID); err == nil {
		el.MustClick()
		log.Println("   -> Значение добавлено в фильтр (кнопка add)")
		return
	}
	elements, err := page.Elements(`button[title="Добавить элемент выбранный из списка"]`)
	if err == nil {
		for _, el := range elements {
			if visible, _ := el.Visible(); visible {
				el.MustClick()
				log.Println("   -> Значение добавлено в фильтр (кнопка add по title)")
				return
			}
		}
	}
	saveErrorScreenshot(page, stepName+"_no_add_button")
	panic("Не найдена кнопка добавления элемента")
}

// clickEmptyPlace клик по пустому месту (label "Описание"), чтобы закрыть подсказки
func clickEmptyPlace(page *rod.Page) {
	if el, err := page.Timeout(3 * time.Second).ElementX("//label[text()='Описание']"); err == nil {
		el.MustClick()
		time.Sleep(500 * time.Millisecond)
	}
}

// selectAndAddSuggest вводит текст, выбирает вариант из списка и нажимает кнопку добавления
func selectAndAddSuggest(page *rod.Page, preferredNum string, text string, addBtnID string, stepName string) {
	var inputEl *rod.Element
	if el, err := page.Timeout(5 * time.Second).Element("#gihyoe_" + preferredNum + "-suggest-input"); err == nil {
		inputEl = el
	} else {
		elements, err := page.Elements("input.autosuggest-input")
		if err != nil || len(elements) == 0 {
			saveErrorScreenshot(page, stepName+"_no_input")
			panic("Не найдено поле ввода autosuggest")
		}
		for i := len(elements) - 1; i >= 0; i-- {
			if visible, _ := elements[i].Visible(); visible {
				inputEl = elements[i]
				break
			}
		}
		if inputEl == nil {
			saveErrorScreenshot(page, stepName+"_no_visible_input")
			panic("Нет видимого поля ввода autosuggest")
		}
	}

	// MustAttribute возвращает *string — разыменовываем с проверкой
	attrID := inputEl.MustAttribute("id")
	if attrID == nil || *attrID == "" {
		saveErrorScreenshot(page, stepName+"_no_input_id")
		panic("Не удалось получить ID поля ввода")
	}
	inputID := *attrID
	base := strings.TrimSuffix(inputID, "-suggest-input")
	optionSel := "#" + base + "-suggest-select option"
	listBtnXPath := fmt.Sprintf(`//*[@id="%s"]/following-sibling::div//button[contains(@class,"autosuggest-list-button")]`, inputID)

	log.Printf("   -> Используем поле ввода: #%s", inputID)
	inputEl.MustClick()
	time.Sleep(300 * time.Millisecond)
	inputEl.MustInput(text)
	time.Sleep(1 * time.Second)

	// Ждем появления варианта; если не появился сам — открываем список кнопкой
	option, err := page.Timeout(5 * time.Second).Element(optionSel)
	if err != nil {
		log.Println("   -> Список не появился сам, открываем кнопкой списка...")
		listBtn := page.MustElementX(listBtnXPath)
		listBtn.MustClick()
		option, err = page.Timeout(10 * time.Second).Element(optionSel)
		if err != nil {
			saveErrorScreenshot(page, stepName+"_no_option")
			panic(fmt.Sprintf("Вариант выбора не появился для текста '%s'", text))
		}
	}
	option.MustClick()
	log.Println("   -> Вариант выбран кликом")
	time.Sleep(500 * time.Millisecond)
	clickAddButton(page, addBtnID, stepName)
	time.Sleep(500 * time.Millisecond)
}

// === ОСНОВНОЙ СЦЕНАРИЙ ИД ===

// runIDAutomation выполняет полный сценарий выгрузки ИД.
// Возвращает true при успехе, false при любой ошибке (программа НЕ закрывается).
func runIDAutomation() (ok bool) {
	cfg := appConfig

	if strings.TrimSpace(cfg.IDPath) == "" {
		updateStatusError("❌ ИД: не указана папка выгрузки (id_path в настройках)")
		return false
	}

	profileDir := filepath.Join(os.TempDir(), "rca-rod-ID")

	log.Println("🚀 Запуск автоматизации RCA (ИД)...")
	updateStatus("🚀 Запуск выгрузки ИД...", 2)

	log.Printf("📁 Папка профиля браузера ИД: %s", profileDir)

	if err := os.RemoveAll(profileDir); err != nil {
		log.Printf("⚠️ Не удалось удалить старую папку профиля: %v", err)
	}

	var page *rod.Page

	defer func() {
		if r := recover(); r != nil {
			log.Printf("❌ ИД ОСТАНОВИЛАСЬ С ОШИБКОЙ: %v", r)
			updateStatusError(fmt.Sprintf("❌ Ошибка ИД: %v", r))
			saveErrorScreenshot(page, "id_crash")
			ok = false
		}
		if err := os.RemoveAll(profileDir); err == nil {
			log.Println("🧹 Папка профиля ИД удалена")
		}
	}()

	updateStatus("📖 Чтение конфигурации...", 6)
	log.Printf("📂 Сетевая папка для выгрузок ИД: %s", cfg.IDPath)

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
		updateStatusError("❌ ИД: не удалось запустить браузер")
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
		updateStatusError("❌ ИД: ошибка нажатия Enter после пароля")
		saveErrorScreenshot(page, "id_login_enter")
		return false
	}

	time.Sleep(4 * time.Second)
	updateStatus("✅ Вход выполнен", 30)

	// 3. Переход к реестру исполнительной документации
	updateStatus("📂 Переход к реестру ИД...", 35)
	page.MustNavigate("https://rca.sgaz.pro/faces/page/contracts/52931/build-tracker/ins-docs/registry")
	page.MustWaitLoad()

	log.Println("   -> Ожидание отрисовки кнопки фильтров...")
	filterBtn := findCSS(page, 15*time.Second, "id_filter_button",
		`button[title="Фильтры для исполнительной документации"]`,
		"#gw9gh4_11",
		"#gw9gh4_3")
	time.Sleep(1 * time.Second)

	// 4. Открытие фильтра
	updateStatus("🔽 Открытие фильтров...", 40)
	filterBtn.MustClick()

	log.Println("   -> Ожидание открытия диалога...")
	typeIDLink := findX(page, 10*time.Second, "id_type_id_link",
		"//a[contains(text(),'Тип ИД')]",
		"//*[@id='q58pxb_12']",
		"//*[@id='q58pxb_13']")
	time.Sleep(500 * time.Millisecond)

	// 5. Клик по ссылке "Тип ИД"
	updateStatus("📋 Открываем блок 'Тип ИД'...", 45)
	typeIDLink.MustClick()
	time.Sleep(500 * time.Millisecond)

	// 6. Первое значение "Тип ИД"
	updateStatus("📋 Добавляем 1-е значение 'Тип ИД'...", 50)
	selectAndAddSuggest(page, "6", "Акт освидетельствования ответственных конструкций", "b69w9q_32", "id_first")

	// 7. Второе значение "Тип ИД"
	updateStatus("📋 Добавляем 2-е значение 'Тип ИД'...", 55)
	selectAndAddSuggest(page, "6", "скр", "b69w9q_32", "id_second")

	clickEmptyPlace(page)

	// 8. Открытие блока "Марка комплекта"
	updateStatus("🏷️ Открываем блок 'Марка комплекта'...", 60)
	brandLink := findX(page, 5*time.Second, "id_brand_link",
		"//a[contains(text(),'Марка комплекта')]",
		"//*[@id='q58pxb_14']",
		"//*[@id='q58pxb_13']")
	brandLink.MustClick()
	time.Sleep(500 * time.Millisecond)

	// 9. Ввод "км" и добавление марки
	updateStatus("🏷️ Добавляем марку 'км'...", 65)
	selectAndAddSuggest(page, "7", "км", "b69w9q_34", "id_km")

	clickEmptyPlace(page)

	// 10. Нажатие OK
	updateStatus("✅ Нажимаем OK в диалоге...", 70)
	okBtn := findCSS(page, 5*time.Second, "id_ok_button",
		`[id="cjtce8_2::ok"]`,
		`[id="cjtce8::ok"]`)
	okBtn.MustClick()
	log.Println("   -> Ожидание закрытия диалога и обновления страницы (4 сек)...")
	time.Sleep(4 * time.Second)

	// 11. Экспорт в Excel
	updateStatus("📥 Выгрузка в Excel запущена...", 80)
	exportBtn := findCSS(page, 10*time.Second, "id_export_button",
		`div[title="Выгрузка в Excel"]`,
		"#C5xhyht_2",
		"#C5xhyht_1")
	exportBtn.MustClick()

	updateStatus("⏳ Ожидание скачивания файла (до 5 минут)...", 84)
	localFilePath := waitForDownload(downloadDir, "Исполнительная документация от*.xlsx", 5*time.Minute)

	if localFilePath == "" {
		updateStatusError("❌ ИД: файл не скачался за 5 минут")
		saveErrorScreenshot(page, "id_download_timeout")
		return false
	}

	updateStatus("✅ Файл скачан: "+filepath.Base(localFilePath), 92)

	// 12. Копирование в сетевую папку
	if _, err := os.Stat(cfg.IDPath); os.IsNotExist(err) {
		updateStatusError("❌ ИД: сетевая папка недоступна")
		saveErrorScreenshot(page, "id_network_unavailable")
		log.Printf("❌ Сетевая папка недоступна: %v", err)
		return false
	}

	updateStatus("🧹 Очистка старых файлов в сетевой папке...", 95)
	cleanOldFiles(cfg.IDPath, "Исполнительная документация от*.xlsx")

	destFileName := filepath.Base(localFilePath)
	destFilePath := filepath.Join(cfg.IDPath, destFileName)

	updateStatus("📋 Копирование файла в сеть...", 97)
	if err := copyFile(localFilePath, destFilePath); err != nil {
		updateStatusError("❌ ИД: ошибка копирования в сетевую папку")
		saveErrorScreenshot(page, "id_copy_error")
		log.Printf("❌ Ошибка копирования файла в сетевую папку: %v", err)
		return false
	}

	if err := os.Remove(localFilePath); err != nil {
		log.Printf("⚠️ Не удалось удалить локальный файл: %v", err)
	} else {
		log.Println("✅ Локальная копия удалена")
	}

	log.Println("🎉 АВТОМАТИЗАЦИЯ ИД ПОЛНОСТЬЮ ЗАВЕРШЕНА!")
	updateStatus("🎉 Выгрузка ИД завершена", 100)
	return true
}
