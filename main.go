package main

import (
	_ "embed"
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/image/draw"

	"github.com/go-rod/rod"
	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

//go:embed icon.ico
var iconData []byte

var appIcon *walk.Icon

// initAppIcon извлекает встроенную иконку и загружает её для окон программы
func initAppIcon() {
	tmp := filepath.Join(os.TempDir(), "rca-app-icon.ico")
	if err := os.WriteFile(tmp, iconData, 0644); err != nil {
		return
	}
	if ico, err := walk.NewIconFromFile(tmp); err == nil {
		appIcon = ico
	}
}

// === GUI: глобальные переменные главного окна ===
var (
	mw          *walk.MainWindow
	logoView    *walk.ImageView
	settingsBtn *walk.PushButton

	rfiCheck    *walk.CheckBox
	rfiCheckLbl *walk.Label
	idCheck     *walk.CheckBox
	idCheckLbl  *walk.Label

	rfiRowLbl *walk.Label
	rfiBar    *walk.ProgressBar
	rfiPct    *walk.Label
	rfiStat   *walk.Label
	rfiFold   *walk.PushButton

	idRowLbl *walk.Label
	idBar    *walk.ProgressBar
	idPct    *walk.Label
	idStat   *walk.Label
	idFold   *walk.PushButton

	startBtn  *walk.PushButton
	cancelBtn *walk.PushButton
	closeBtn  *walk.PushButton

	isDark   bool
	guiAlive int32
)

var statusChannel = make(chan StatusMessage, 100)

type StatusMessage struct {
	Task         string
	Text         string
	Progress     int
	IsError      bool
	IsFinal      bool
	OpenSettings bool
}

var (
	globalBrowser *rod.Browser
	appConfig     *Config
	configPath    string
	exeDirPath    string

	taskMu      sync.Mutex
	currentTask = "RFI"
)

func setTask(t string) {
	taskMu.Lock()
	currentTask = t
	taskMu.Unlock()
}

func getTask() string {
	taskMu.Lock()
	defer taskMu.Unlock()
	return currentTask
}

func updateStatus(text string, progress int) {
	select {
	case statusChannel <- StatusMessage{Task: getTask(), Text: text, Progress: progress}:
	default:
	}
}

func updateStatusError(text string) {
	select {
	case statusChannel <- StatusMessage{Task: getTask(), Text: text, IsError: true}:
	default:
	}
}

func updateStatusFinal(text string) {
	select {
	case statusChannel <- StatusMessage{Task: getTask(), Text: text, IsFinal: true}:
	default:
	}
}

func requestOpenSettings() {
	select {
	case statusChannel <- StatusMessage{OpenSettings: true}:
	default:
	}
}

// loadLogo подгружает логотип под текущую тему и масштабирует его,
// чтобы он полностью помещался в окне (300x80) с сохранением пропорций.
// Файлы: icon_dark.png (тёмная тема), icon_light.png (светлая), icon.png — запасной.
func loadLogo() {
	name := "icon_light.png"
	if isDark {
		name = "icon_dark.png"
	}
	path := filepath.Join(exeDirPath, name)
	if _, err := os.Stat(path); err != nil {
		path = filepath.Join(exeDirPath, "icon.png")
	}

	f, err := os.Open(path)
	if err != nil {
		logoView.SetVisible(false)
		return
	}
	img, err := png.Decode(f)
	f.Close()
	if err != nil {
		logoView.SetVisible(false)
		return
	}

	// Вписываем в 300x80 с сохранением пропорций
	maxW, maxH := 300, 80
	bounds := img.Bounds()
	bw, bh := bounds.Dx(), bounds.Dy()
	if bw == 0 || bh == 0 {
		logoView.SetVisible(false)
		return
	}
	scale := float64(maxW) / float64(bw)
	if float64(bh)*scale > float64(maxH) {
		scale = float64(maxH) / float64(bh)
	}
	targetW := int(float64(bw) * scale)
	targetH := int(float64(bh) * scale)
	if targetW < 1 || targetH < 1 {
		logoView.SetVisible(false)
		return
	}

	resized := image.NewRGBA(image.Rect(0, 0, targetW, targetH))
	draw.BiLinear.Scale(resized, resized.Bounds(), img, bounds, draw.Over, nil)

	tmpPath := filepath.Join(os.TempDir(), "rca-logo-current.png")
	out, err := os.Create(tmpPath)
	if err != nil {
		logoView.SetVisible(false)
		return
	}
	png.Encode(out, resized)
	out.Close()

	if bmp, err := walk.NewBitmapFromFile(tmpPath); err == nil {
		logoView.SetImage(bmp)
		logoView.SetVisible(true)
	} else {
		logoView.SetVisible(false)
	}
}

// runSelectedTasks выполняет выбранные выгрузки последовательно
func runSelectedTasks() {
	defer func() {
		// После завершения возвращаем управление пользователю
		mw.Synchronize(func() {
			startBtn.SetEnabled(true)
			rfiCheck.SetEnabled(true)
			idCheck.SetEnabled(true)
		})
	}()

	allOK := true

	if rfiCheck.Checked() {
		setTask("RFI")
		if !runRFIAutomation() {
			allOK = false
		}
	}

	if idCheck.Checked() {
		setTask("ID")
		if !runIDAutomation() {
			allOK = false
		}
	}

	if allOK {
		updateStatusFinal("🎉 Все выбранные выгрузки завершены!")
	} else {
		updateStatus("⚠️ Завершено с ошибками — проверьте статусы задач", 0)
	}
}

// applyTheme применяет светлую или тёмную тему ко всем текстам окна,
// обновляет кнопки папок и перезагружает логотип под тему
func applyTheme() {
	var bg, fg walk.Color
	if isDark {
		bg = walk.RGB(38, 38, 42)
		fg = walk.RGB(240, 240, 240)
	} else {
		bg = walk.RGB(250, 250, 250)
		fg = walk.RGB(35, 35, 35)
	}
	if brush, err := walk.NewSolidColorBrush(bg); err == nil {
		mw.SetBackground(brush)
	}
	rfiStat.SetTextColor(fg)
	idStat.SetTextColor(fg)
	rfiPct.SetTextColor(fg)
	idPct.SetTextColor(fg)
	rfiRowLbl.SetTextColor(fg)
	idRowLbl.SetTextColor(fg)
	rfiCheckLbl.SetTextColor(fg)
	idCheckLbl.SetTextColor(fg)

	// Кнопки папок активны, если папка указана в настройках
	rfiFold.SetEnabled(strings.TrimSpace(appConfig.RFIPath) != "")
	idFold.SetEnabled(strings.TrimSpace(appConfig.IDPath) != "")

	// Логотип под тему
	loadLogo()
}

// cancelRun отменяет выполнение и завершает программу
func cancelRun() {
	log.Println("🛑 Выполнение отменено пользователем")
	if globalBrowser != nil {
		globalBrowser.Close()
	}
	os.Exit(0)
}

// startMainWindow создаёт и запускает главное окно программы
func startMainWindow(exeDir, exeName string) {
	windowTitle := strings.ReplaceAll(exeName, "_", " ")

	last := appConfig.LastTasks
	rfiDefault := last == "" || strings.Contains(last, "RFI")
	idDefault := strings.Contains(last, "ID")

	err := MainWindow{
		AssignTo: &mw,
		Title:    windowTitle,
		Size:     Size{550, 400},
		MinSize:  Size{550, 400},
		Layout:   VBox{},
		Children: []Widget{
			Composite{
				Layout: HBox{},
				Children: []Widget{
					HSpacer{},
					PushButton{
						AssignTo: &settingsBtn,
						Text:     "⚙",
						MaxSize:  Size{44, 32},
						OnClicked: func() {
							openSettingsAndMaybeStart()
						},
					},
				},
			},
			ImageView{
				AssignTo: &logoView,
				MinSize:  Size{300, 80},
				MaxSize:  Size{300, 80},
			},
			Composite{
				Layout: HBox{},
				Children: []Widget{
					HSpacer{},
					CheckBox{AssignTo: &rfiCheck, Checked: rfiDefault},
					Label{AssignTo: &rfiCheckLbl, Text: "RFI (инспекции)"},
					HSpacer{},
					CheckBox{AssignTo: &idCheck, Checked: idDefault},
					Label{AssignTo: &idCheckLbl, Text: "ИД (исполнительная документация)"},
					HSpacer{},
				},
			},
			Composite{
				Layout: HBox{},
				Children: []Widget{
					Label{AssignTo: &rfiRowLbl, Text: "RFI", MinSize: Size{40, 0}},
					ProgressBar{AssignTo: &rfiBar, MinSize: Size{300, 12}, MaxSize: Size{1000, 12}},
					Label{AssignTo: &rfiPct, Text: "0%", MinSize: Size{50, 0}},
					PushButton{
						AssignTo:  &rfiFold,
						Text:      "📁",
						MaxSize:   Size{44, 30},
						Enabled:   false,
						OnClicked: func() { openFolder(appConfig.RFIPath) },
					},
				},
			},
			Label{
				AssignTo:  &rfiStat,
				Text:      "Ожидание запуска...",
				Alignment: AlignHCenterVCenter,
				MinSize:   Size{0, 30},
			},
			Composite{
				Layout: HBox{},
				Children: []Widget{
					Label{AssignTo: &idRowLbl, Text: "ИД ", MinSize: Size{40, 0}},
					ProgressBar{AssignTo: &idBar, MinSize: Size{300, 12}, MaxSize: Size{1000, 12}},
					Label{AssignTo: &idPct, Text: "0%", MinSize: Size{50, 0}},
					PushButton{
						AssignTo:  &idFold,
						Text:      "📁",
						MaxSize:   Size{44, 30},
						Enabled:   false,
						OnClicked: func() { openFolder(appConfig.IDPath) },
					},
				},
			},
			Label{
				AssignTo:  &idStat,
				Text:      "Ожидание запуска...",
				Alignment: AlignHCenterVCenter,
				MinSize:   Size{0, 30},
			},
			Composite{
				Layout: HBox{},
				Children: []Widget{
					HSpacer{},
					PushButton{
						AssignTo: &startBtn,
						Text:     "Старт",
						MinSize:  Size{120, 36},
						OnClicked: func() {
							tasks := ""
							if rfiCheck.Checked() {
								tasks = "RFI"
							}
							if idCheck.Checked() {
								if tasks != "" {
									tasks += ","
								}
								tasks += "ID"
							}
							if tasks == "" {
								return
							}
							appConfig.LastTasks = tasks
							SaveConfig(configPath, appConfig)

							startBtn.SetEnabled(false)
							rfiCheck.SetEnabled(false)
							idCheck.SetEnabled(false)

							rfiBar.SetValue(0)
							rfiPct.SetText("0%")
							rfiStat.SetText("Ожидание запуска...")
							idBar.SetValue(0)
							idPct.SetText("0%")
							idStat.SetText("Ожидание запуска...")

							go runSelectedTasks()
						},
					},
					HSpacer{},
					PushButton{
						AssignTo:  &cancelBtn,
						Text:      "Отмена",
						MinSize:   Size{120, 36},
						OnClicked: cancelRun,
					},
					HSpacer{},
					PushButton{
						AssignTo:  &closeBtn,
						Text:      "Закрыть",
						MinSize:   Size{120, 36},
						OnClicked: func() { mw.Close() },
					},
					HSpacer{},
				},
			},
		},
	}.Create()

	if err != nil {
		log.Printf("⚠️ Не удалось создать окно: %v", err)
		return
	}

	if appIcon != nil {
		mw.SetIcon(appIcon)
	}

	applyTheme()

	go func() {
		for msg := range statusChannel {
			m := msg
			if atomic.LoadInt32(&guiAlive) == 0 {
				continue
			}
			if m.OpenSettings {
				mw.Synchronize(openSettingsAndMaybeStart)
				continue
			}
			mw.Synchronize(func() {
				var stat *walk.Label
				var bar *walk.ProgressBar
				var pct *walk.Label
				var fold *walk.PushButton

				if m.Task == "ID" {
					stat, bar, pct, fold = idStat, idBar, idPct, idFold
				} else {
					stat, bar, pct, fold = rfiStat, rfiBar, rfiPct, rfiFold
				}

				if m.Text != "" {
					stat.SetText(m.Text)
				}
				if m.Progress > 0 {
					bar.SetValue(m.Progress)
					pct.SetText(fmt.Sprintf("%d%%", m.Progress))
				}
				if m.Progress == 100 && !m.IsError {
					fold.SetEnabled(true)
				}
				if m.IsFinal {
					cancelBtn.SetEnabled(false)
					time.AfterFunc(5*time.Second, func() {
						mw.Synchronize(func() { mw.Close() })
					})
				}
			})
		}
	}()

	atomic.StoreInt32(&guiAlive, 1)
	mw.Run()
	atomic.StoreInt32(&guiAlive, 0)
}

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

func main() {
	absCwd, err := filepath.Abs(".")
	if err != nil {
		log.Fatalf("❌ Не удалось получить абсолютный путь: %v", err)
	}

	exePath, err := os.Executable()
	if err != nil {
		log.Fatalf("❌ Не удалось определить путь к exe-файлу: %v", err)
	}
	exeName := strings.TrimSuffix(filepath.Base(exePath), filepath.Ext(exePath))
	exeDir := filepath.Dir(exePath)
	exeDirPath = exeDir

	initAppIcon()

	// === ЗАЩИТА ОТ ВТОРОГО ЗАПУСКА ===
	inst := acquireSingleInstance(exeName + "_SingleInstance")
	if inst == nil {
		msgBox("RCA Automator", "Программа уже запущена.", mbIconInfo)
		return
	}

	// === ЛОГИ ===
	logFile, err := setupLogging(absCwd)
	if err != nil {
		log.Println("⚠️ Не удалось создать лог-файл, пишем только в консоль")
	}

	// === ЧТЕНИЕ КОНФИГА ===
	configPath = filepath.Join(absCwd, "config.txt")
	cfg, err := LoadConfig(configPath)
	if err != nil {
		log.Printf("⚠️ config.txt не найден или не читается: %v", err)
		cfg = &Config{}
	}
	appConfig = cfg
	isDark = cfg.Theme == "dark"

	// === ЗАПУСК ГЛАВНОГО ОКНА ===
	go startMainWindow(exeDir, exeName)
	time.Sleep(300 * time.Millisecond)

	// Если конфиг неполный — автоматически открываем настройки
	if !cfg.IsComplete() {
		requestOpenSettings()
	}

	// Держим процесс живым, пока работает GUI
	for atomic.LoadInt32(&guiAlive) == 1 {
		time.Sleep(200 * time.Millisecond)
	}

	if logFile != nil {
		logFile.Close()
	}
}
