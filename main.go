package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

func findBrowser() string {
	paths := []string{
		`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
		`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		`C:\Users\%USERNAME%\AppData\Local\Google\Chrome\Application\chrome.exe`,
		`C:\Users\%USERNAME%\AppData\Local\Microsoft\Edge\Application\msedge.exe`,
	}
	for _, p := range paths {
		p = os.ExpandEnv(p)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "chrome"
}

// readConfig читает логин, пароль и сетевой путь из config.txt
func readConfig() (string, string, string, error) {
	data, err := os.ReadFile("config.txt")
	if err != nil {
		return "", "", "", fmt.Errorf("не удалось прочитать config.txt: %v", err)
	}

	var login, password, networkPath string
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "login=") {
			login = strings.TrimPrefix(line, "login=")
		} else if strings.HasPrefix(line, "password=") {
			password = strings.TrimPrefix(line, "password=")
		} else if strings.HasPrefix(line, "network_path=") {
			networkPath = strings.TrimPrefix(line, "network_path=")
		}
	}

	if login == "" || password == "" {
		return "", "", "", fmt.Errorf("логин или пароль не найдены в config.txt")
	}
	if networkPath == "" {
		return "", "", "", fmt.Errorf("network_path не найден в config.txt")
	}
	return login, password, networkPath, nil
}

// cleanOldScreenshots удаляет старые скриншоты при старте скрипта
func cleanOldScreenshots(dir string) {
	removed := 0
	for _, pattern := range []string{"step_*.png", "error_*.png"} {
		matches, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil {
			continue
		}
		for _, file := range matches {
			if err := os.Remove(file); err == nil {
				removed++
			}
		}
	}
	if removed > 0 {
		log.Printf("🧹 Удалено старых скриншотов: %d", removed)
	}
}

// cleanRodTempDirs удаляет старые случайные папки профиля go-rod в Temp\rod\user-data
// (только папки от завершённых запусков — занятые работающим браузером Windows не удалит)
func cleanRodTempDirs() {
	rodTemp := filepath.Join(os.TempDir(), "rod", "user-data")
	entries, err := os.ReadDir(rodTemp)
	if err != nil {
		return // папки нет — чистить нечего
	}
	removed := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if err := os.RemoveAll(filepath.Join(rodTemp, entry.Name())); err == nil {
			removed++
		}
	}
	if removed > 0 {
		log.Printf("🧹 Удалено старых папок go-rod в Temp: %d", removed)
	}
}

// saveErrorScreenshot сохраняет скриншот ТОЛЬКО при ошибке
func saveErrorScreenshot(page *rod.Page, stepName string) {
	if page == nil {
		return
	}
	cwd, _ := os.Getwd()
	if cwd == "" {
		cwd = "."
	}
	path := filepath.Join(cwd, fmt.Sprintf("error_%s.png", stepName))
	imgBytes, err := page.Screenshot(false, nil)
	if err != nil {
		log.Printf("⚠️ Не удалось сделать скриншот ошибки: %v", err)
		return
	}
	if err := os.WriteFile(path, imgBytes, 0644); err != nil {
		log.Printf("⚠️ Не удалось сохранить скриншот ошибки: %v", err)
		return
	}
	log.Printf("📸 Скриншот ошибки сохранен: %s", path)
}

func sendKeyThroughCDP(page *rod.Page, key string, eventType proto.InputDispatchKeyEventType) error {
	return proto.InputDispatchKeyEvent{
		Type:                  eventType,
		Key:                   key,
		Code:                  key,
		WindowsVirtualKeyCode: 13,
		NativeVirtualKeyCode:  13,
	}.Call(page)
}

// waitForDownload ищет самый свежий файл по маске в папке
func waitForDownload(dir string, pattern string, timeout time.Duration) string {
	startTime := time.Now()
	log.Printf("🔍 Начинаю мониторинг папки: %s", dir)
	log.Printf("🔍 Ищу файлы по маске: %s", pattern)

	for {
		if time.Since(startTime) > timeout {
			log.Printf("⏰ Время ожидания истекло (%v)", timeout)
			return ""
		}

		matches, err := filepath.Glob(filepath.Join(dir, pattern))
		if err == nil && len(matches) > 0 {
			var newestFile string
			var newestTime time.Time

			for _, match := range matches {
				if strings.HasSuffix(match, ".crdownload") {
					continue
				}

				info, err := os.Stat(match)
				if err != nil {
					continue
				}

				if newestFile == "" || info.ModTime().After(newestTime) {
					newestFile = match
					newestTime = info.ModTime()
				}
			}

			if newestFile != "" {
				log.Printf("✅ Найден свежий файл: %s", newestFile)
				return newestFile
			}
		}

		time.Sleep(1 * time.Second)
	}
}

// cleanOldFiles удаляет старые файлы выгрузки в сетевой папке по маске
func cleanOldFiles(dir string, pattern string) {
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		log.Printf("⚠️ Ошибка поиска старых файлов по маске '%s': %v", pattern, err)
		return
	}

	if len(matches) == 0 {
		log.Println("   -> Старых файлов для удаления не найдено")
		return
	}

	for _, file := range matches {
		log.Printf("🗑️ Удаляем старый файл: %s", filepath.Base(file))
		if err := os.Remove(file); err != nil {
			log.Printf("⚠️ Не удалось удалить файл %s: %v", file, err)
		}
	}
}

// copyFile копирует файл из src в dst
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("не удалось открыть исходный файл: %v", err)
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("не удалось создать файл назначения: %v", err)
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, sourceFile); err != nil {
		return fmt.Errorf("ошибка копирования данных: %v", err)
	}

	return destFile.Sync()
}

func main() {
	absCwd, err := filepath.Abs(".")
	if err != nil {
		log.Fatalf("❌ Не удалось получить абсолютный путь: %v", err)
	}

	// Лог очищается при каждом запуске
	logFilePath := filepath.Join(absCwd, "debug.log")
	logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
	if err == nil {
		multiWriter := io.MultiWriter(os.Stdout, logFile)
		log.SetOutput(multiWriter)
	} else {
		log.Println("⚠️ Не удалось создать debug.log, пишем только в консоль")
	}

	log.Println("🚀 Запуск автоматизации RCA...")

	// === УНИКАЛЬНАЯ ПАПКА ПРОФИЛЯ БРАУЗЕРА НА ОСНОВЕ ИМЕНИ EXE ===
	exePath, err := os.Executable()
	if err != nil {
		log.Fatalf("❌ Не удалось определить путь к exe-файлу: %v", err)
	}
	exeName := strings.TrimSuffix(filepath.Base(exePath), filepath.Ext(exePath))
	profileDir := filepath.Join(os.TempDir(), "rca-rod-"+exeName)
	log.Printf("📁 Папка профиля браузера: %s", profileDir)

	// Удаляем папку профиля, если она осталась от прошлого аварийного запуска
	if err := os.RemoveAll(profileDir); err != nil {
		log.Printf("⚠️ Не удалось удалить старую папку профиля: %v", err)
	}

	// Разовая очистка накопившегося мусора go-rod в Temp\rod\user-data
	cleanRodTempDirs()

	// Автоудаление старых скриншотов при старте
	cleanOldScreenshots(absCwd)

	var page *rod.Page

	defer func() {
		if r := recover(); r != nil {
			log.Printf("❌ СКРИПТ ОСТАНОВИЛСЯ С ОШИБКОЙ: %v", r)
			saveErrorScreenshot(page, "crash")
		}
		// Удаляем папку профиля после завершения работы браузера
		if profileDir != "" {
			if err := os.RemoveAll(profileDir); err != nil {
				log.Printf("⚠️ Не удалось удалить папку профиля: %v", err)
			} else {
				log.Println("🧹 Папка профиля браузера удалена")
			}
		}
		if logFile != nil {
			logFile.Close()
		}
	}()

	// Читаем логин, пароль и сетевой путь из config.txt
	login, password, networkSharePath, err := readConfig()
	if err != nil {
		log.Fatalf("❌ Ошибка конфигурации: %v", err)
	}
	log.Printf("📂 Сетевая папка для выгрузок: %s", networkSharePath)

	homeDir, _ := os.UserHomeDir()
	downloadDir := filepath.Join(homeDir, "Downloads")
	if _, err := os.Stat(downloadDir); os.IsNotExist(err) {
		downloadDir = filepath.Join(homeDir, "Загрузки")
	}
	log.Printf("📂 Отслеживаем папку загрузок: %s", downloadDir)

	browserPath := findBrowser()
	log.Printf("🌐 Используем браузер: %s", browserPath)

	l := launcher.New().
		Bin(browserPath).
		UserDataDir(profileDir). // Фиксированная уникальная папка профиля
		Set("download.default_directory", filepath.ToSlash(downloadDir)).
		Set("download.prompt_for_download", "false").
		Set("safebrowsing.enabled", "false").
		Headless(true). // ФОНОВЫЙ РЕЖИМ: окно браузера не показывается
		// Если понадобится снова увидеть окно браузера:
		// закомментируйте Headless(true) выше и раскомментируйте Headless(false) ниже:
		// Headless(false).
		Devtools(false)

	url, err := l.Launch()
	if err != nil {
		log.Fatalf("❌ Не удалось запустить браузер: %v", err)
	}

	browser := rod.New().ControlURL(url).MustConnect()
	defer browser.MustClose()

	page = browser.MustPage()

	// 1. Настройка экрана
	log.Println("📐 Устанавливаем viewport 1920x1080...")
	page.MustSetViewport(1920, 1080, 1, false)
	time.Sleep(500 * time.Millisecond)

	// 2. Вход в систему
	log.Println("🔑 Выполняем вход...")
	page.MustNavigate("https://rca.sgaz.pro/")
	page.MustWaitLoad()

	page.MustElement("#loginInput").MustWaitVisible().MustInput(login)
	page.MustElement("#passInput").MustWaitVisible().MustInput(password)

	if err := page.Keyboard.Press(input.Enter); err != nil {
		saveErrorScreenshot(page, "login_enter")
		log.Fatalf("❌ Ошибка нажатия Enter после пароля: %v", err)
	}

	time.Sleep(4 * time.Second)

	// 3. Переход к задачам
	log.Println("📂 Переход к задачам...")
	page.MustNavigate("https://rca.sgaz.pro/faces/page/contracts/52931/build-tracker/tasks")

	log.Println("   -> Ожидание отрисовки кнопки фильтров...")
	filterBtn := page.MustElement("#gw9gh4_9").MustWaitVisible()

	// 4. Открытие фильтра
	log.Println("🔽 Открываем фильтры...")
	filterBtn.MustClick()

	log.Println("   -> Ожидание открытия диалога...")
	filterInput := page.MustElement("#gihyoe-suggest-input").MustWaitVisible()
	time.Sleep(500 * time.Millisecond)

	// 5. Применение фильтра "км"
	log.Println("⌨️ Применяем фильтр 'км'...")
	filterInput.MustClick()
	time.Sleep(300 * time.Millisecond)
	filterInput.MustInput("км")

	time.Sleep(2 * time.Second)

	log.Println("   -> Отправка Enter через CDP...")
	sendKeyThroughCDP(page, "Enter", proto.InputDispatchKeyEventTypeKeyDown)
	time.Sleep(100 * time.Millisecond)
	sendKeyThroughCDP(page, "Enter", proto.InputDispatchKeyEventTypeKeyUp)

	time.Sleep(2 * time.Second)

	// 6. Нажатие OK
	log.Println("✅ Нажимаем OK в диалоге...")
	page.MustElement(`[id="cjtce8::ok"]`).MustWaitVisible().MustClick()

	log.Println("   -> Ожидание закрытия диалога и обновления страницы (4 сек)...")
	time.Sleep(4 * time.Second)

	// 7. Выбор типа заявки RFI
	log.Println("📋 Открываем выпадающий список типов заявок...")
	dropdownTarget := page.MustElement("#hyj3sh-dropdown-target").MustWaitVisible()
	dropdownTarget.MustScrollIntoView()
	time.Sleep(300 * time.Millisecond)
	dropdownTarget.MustClick()

	log.Println("   -> Ожидание открытия списка (4 сек)...")
	time.Sleep(4 * time.Second)

	log.Println("🎯 Выбираем 'RFI (строительный контроль)'...")
	rfiSelected := false

	rfiElement, err := page.ElementX("//html/body/div[5]/div[4]/span")
	if err == nil && rfiElement != nil {
		rfiElement.MustClick()
		rfiSelected = true
		log.Println("   -> Выбрано через XPath")
	}

	if !rfiSelected {
		elements, err := page.ElementsX("//span[contains(text(), 'RFI (строительный контроль)')]")
		if err == nil && len(elements) > 0 {
			elements[0].MustClick()
			rfiSelected = true
			log.Println("   -> Выбрано через текст")
		}
	}

	if !rfiSelected {
		saveErrorScreenshot(page, "rfi_not_selected")
		log.Println("⚠️ Не удалось выбрать RFI!")
	}

	time.Sleep(1 * time.Second)

	// 8. Очистка даты
	log.Println("📅 Очищаем поле даты...")
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
		log.Println("   -> Дата очищена")
	}

	time.Sleep(500 * time.Millisecond)

	// 9. Поиск "SMU"
	log.Println("🔍 Вводим 'SMU' в поиск...")
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
		log.Println("   -> SMU введен")
	}

	time.Sleep(500 * time.Millisecond)

	log.Println("   -> Нажатие Enter через CDP для поиска...")
	sendKeyThroughCDP(page, "Enter", proto.InputDispatchKeyEventTypeKeyDown)
	time.Sleep(100 * time.Millisecond)
	sendKeyThroughCDP(page, "Enter", proto.InputDispatchKeyEventTypeKeyUp)

	time.Sleep(3 * time.Second)

	// ==========================================
	// 10. СКАЧИВАНИЕ И ПЕРЕМЕЩЕНИЕ ФАЙЛА
	// ==========================================
	log.Println("📥 Начинаем процесс выгрузки в Excel...")

	log.Println("   -> Нажимаем кнопку 'Выгрузка в Excel'...")
	page.MustElement("#C5xhyht").MustWaitVisible().MustClick()

	log.Printf("   -> Ожидание завершения скачивания в %s (макс. 5 минут)...", downloadDir)

	localFilePath := waitForDownload(downloadDir, "Инспекции на*.xlsx", 5*time.Minute)

	if localFilePath == "" {
		saveErrorScreenshot(page, "download_timeout")
		log.Fatalf("❌ Файл не был скачан в течение 5 минут!")
	}

	log.Printf("✅ Файл успешно скачан: %s", localFilePath)

	// ПРОВЕРКА И КОПИРОВАНИЕ В СЕТЕВУЮ ПАПКУ (путь из config.txt)
	log.Printf("📂 Копирование файла в сетевую папку: %s", networkSharePath)

	if _, err := os.Stat(networkSharePath); os.IsNotExist(err) {
		saveErrorScreenshot(page, "network_unavailable")
		log.Fatalf("❌ Сетевая папка недоступна: %v", err)
	}

	log.Println("🧹 Очистка старых файлов выгрузки в сетевой папке...")
	cleanOldFiles(networkSharePath, "Инспекции на*.xlsx")

	destFileName := filepath.Base(localFilePath)
	destFilePath := filepath.Join(networkSharePath, destFileName)

	log.Printf("📋 Копируем %s -> %s", destFileName, networkSharePath)
	if err := copyFile(localFilePath, destFilePath); err != nil {
		saveErrorScreenshot(page, "copy_error")
		log.Fatalf("❌ Ошибка копирования файла в сетевую папку: %v", err)
	}
	log.Println("✅ Файл успешно скопирован в сетевую папку!")

	log.Println("🗑️ Удаляем скачанный файл из папки Загрузок...")
	if err := os.Remove(localFilePath); err != nil {
		log.Printf("⚠️ Не удалось удалить локальный файл (возможно, он открыт): %v", err)
	} else {
		log.Println("✅ Локальная копия удалена")
	}

	log.Println("🎉 АВТОМАТИЗАЦИЯ ПОЛНОСТЬЮ ЗАВЕРШЕНА!")
	log.Println("💡 Актуальный файл находится в сетевой папке.")
}
