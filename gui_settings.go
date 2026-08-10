package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

var settingsDlg *walk.Dialog

// openSettingsWindow открывает окно настроек (с владельцем — главным окном)
func openSettingsWindow() {
	// Если предыдущее окно настроек ещё открыто — закрываем его
	if settingsDlg != nil {
		old := settingsDlg
		settingsDlg = nil
		func() {
			defer func() { _ = recover() }()
			old.Cancel()
		}()
	}

	var dlg *walk.Dialog
	var loginEdit *walk.LineEdit
	var passEdit *walk.LineEdit
	var rfiEdit *walk.LineEdit
	var idEdit *walk.LineEdit
	var themeChk *walk.CheckBox
	var checkBtn *walk.PushButton
	var statusLbl *walk.Label

	updateCheckBtn := func() {
		checkBtn.SetEnabled(strings.TrimSpace(loginEdit.Text()) != "" && passEdit.Text() != "")
	}

	err := Dialog{
		AssignTo:  &dlg,
		Title:     "Настройки RCA Automator",
		FixedSize: true,
		MinSize:   Size{480, 420},
		Layout:    VBox{},
		Children: []Widget{
			Composite{
				Layout: HBox{},
				Children: []Widget{
					Label{Text: "Логин:", MinSize: Size{130, 0}},
					LineEdit{AssignTo: &loginEdit, MinSize: Size{280, 0}},
				},
			},
			Composite{
				Layout: HBox{},
				Children: []Widget{
					Label{Text: "Пароль:", MinSize: Size{130, 0}},
					LineEdit{AssignTo: &passEdit, PasswordMode: true, MinSize: Size{280, 0}},
				},
			},
			Composite{
				Layout: HBox{},
				Children: []Widget{
					Label{Text: "Папка выгрузки RFI:", MinSize: Size{130, 0}},
					LineEdit{AssignTo: &rfiEdit, MinSize: Size{280, 0}},
				},
			},
			Composite{
				Layout: HBox{},
				Children: []Widget{
					Label{Text: "Папка выгрузки ИД:", MinSize: Size{130, 0}},
					LineEdit{AssignTo: &idEdit, MinSize: Size{280, 0}},
				},
			},
			CheckBox{
				AssignTo: &themeChk,
				Text:     "Тёмная тема",
			},
			Composite{
				Layout: HBox{},
				Children: []Widget{
					PushButton{
						AssignTo: &checkBtn,
						Text:     "Проверить вход",
						MinSize:  Size{140, 32},
						Enabled:  false,
						OnClicked: func() {
							login := strings.TrimSpace(loginEdit.Text())
							pass := passEdit.Text()
							checkBtn.SetEnabled(false)
							statusLbl.SetText("⏳ Проверка подключения...")
							go func() {
								ok, msg := verifyLogin(login, pass)
								// Результат передаём через главное окно (mw.Synchronize) —
								// этот механизм гарантированно работает
								func() {
									defer func() { _ = recover() }()
									mw.Synchronize(func() {
										if ok {
											statusLbl.SetText("✅ " + msg)
										} else {
											statusLbl.SetText("❌ " + msg)
										}
										updateCheckBtn()
									})
								}()
							}()
						},
					},
					Label{AssignTo: &statusLbl, Text: ""},
				},
			},
			Composite{
				Layout: HBox{},
				Children: []Widget{
					HSpacer{},
					PushButton{
						Text:    "Сохранить",
						MinSize: Size{120, 34},
						OnClicked: func() {
							appConfig.Login = strings.TrimSpace(loginEdit.Text())
							appConfig.Password = passEdit.Text()
							appConfig.RFIPath = strings.TrimSpace(rfiEdit.Text())
							appConfig.IDPath = strings.TrimSpace(idEdit.Text())
							appConfig.Theme = "light"
							if themeChk.Checked() {
								appConfig.Theme = "dark"
							}
							if err := SaveConfig(configPath, appConfig); err != nil {
								statusLbl.SetText("❌ Не удалось сохранить: " + err.Error())
								return
							}

							// Применяем тему. Запуск — только через кнопку «Старт»
							isDark = appConfig.Theme == "dark"
							applyTheme()
							settingsDlg = nil
							dlg.Accept()
						},
					},
					HSpacer{},
					PushButton{
						Text:    "Отмена",
						MinSize: Size{120, 34},
						OnClicked: func() {
							settingsDlg = nil
							dlg.Cancel()
						},
					},
					HSpacer{},
				},
			},
		},
	}.Create(mw)

	if err != nil {
		log.Printf("⚠️ Не удалось создать окно настроек: %v", err)
		return
	}

	if appIcon != nil {
		dlg.SetIcon(appIcon)
	}

	settingsDlg = dlg

	// Предзаполнение из текущего конфига
	loginEdit.SetText(appConfig.Login)
	passEdit.SetText(appConfig.Password)
	rfiEdit.SetText(appConfig.RFIPath)
	idEdit.SetText(appConfig.IDPath)
	themeChk.SetChecked(appConfig.Theme == "dark")
	updateCheckBtn()

	loginEdit.TextChanged().Attach(func() { updateCheckBtn() })
	passEdit.TextChanged().Attach(func() { updateCheckBtn() })

	dlg.Show()
}

// openSettingsAndMaybeStart — совместимая обёртка для главного окна
func openSettingsAndMaybeStart() {
	openSettingsWindow()
}

// verifyLogin проверяет логин и пароль реальным входом в систему.
// Все операции — с таймаутами, чтобы проверка не могла «зависнуть» навсегда.
func verifyLogin(login, password string) (ok bool, msg string) {
	defer func() {
		if r := recover(); r != nil {
			ok = false
			msg = "не удалось подключиться к сайту (таймаут)"
		}
	}()

	profileDir := filepath.Join(os.TempDir(), "rca-rod-test")
	os.RemoveAll(profileDir)
	defer os.RemoveAll(profileDir)

	l := launcher.New().
		Bin(findBrowser()).
		UserDataDir(profileDir).
		Headless(true)

	url, err := l.Launch()
	if err != nil {
		return false, "не удалось запустить браузер"
	}

	browser := rod.New().ControlURL(url)
	if err := browser.Connect(); err != nil {
		return false, "не удалось подключиться к браузеру"
	}
	defer browser.MustClose()

	page := browser.MustPage()

	// Все последующие Must-операции — с таймаутом 20 секунд
	page = page.Timeout(20 * time.Second)

	page.MustNavigate("https://rca.sgaz.pro/")
	page.MustWaitLoad()

	page.MustElement("#loginInput").MustInput(login)
	page.MustElement("#passInput").MustInput(password)
	page.Keyboard.Press(input.Enter)

	time.Sleep(5 * time.Second)

	// Если поле логина исчезло — вход выполнен (Element без таймаута — мгновенный запрос)
	if el, err := page.Element("#loginInput"); err == nil && el != nil {
		return false, "логин или пароль неверны"
	}

	return true, "вход выполнен успешно"
}
