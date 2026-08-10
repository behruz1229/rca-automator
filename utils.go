package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// safeWriter никогда не возвращает ошибку, чтобы не прерывать MultiWriter
type safeWriter struct {
	w io.Writer
}

func (s safeWriter) Write(p []byte) (int, error) {
	s.w.Write(p)
	return len(p), nil
}

// setupLogging создаёт папку logs и файл лога с датой и временем запуска,
// после чего оставляет не более 5 самых свежих логов
func setupLogging(absCwd string) (*os.File, error) {
	logsDir := filepath.Join(absCwd, "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return nil, err
	}

	// Имя файла: debug_2026-08-09_15-30.log
	name := fmt.Sprintf("debug_%s.log", time.Now().Format("2006-01-02_15-04"))
	logFile, err := os.OpenFile(filepath.Join(logsDir, name), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
	if err != nil {
		return nil, err
	}

	log.SetOutput(io.MultiWriter(logFile, safeWriter{os.Stdout}))

	// Оставляем только 5 самых свежих логов (текущий — самый свежий, он останется)
	pruneOldLogs(logsDir, 5)

	return logFile, nil
}

// pruneOldLogs оставляет в папке только maxKeep самых свежих логов
func pruneOldLogs(dir string, maxKeep int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	var logs []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "debug_") || !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		logs = append(logs, e.Name())
	}

	if len(logs) <= maxKeep {
		return
	}

	// Сортируем по имени по убыванию: формат имени "дата_время" сортируется хронологически
	sort.Slice(logs, func(i, j int) bool { return logs[i] > logs[j] })

	// Удаляем всё, кроме первых maxKeep (самых свежих)
	for _, name := range logs[maxKeep:] {
		os.Remove(filepath.Join(dir, name))
	}
}

// openFolder открывает проводник Windows с указанной папкой
func openFolder(path string) {
	exec.Command("explorer.exe", path).Start()
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
func cleanRodTempDirs() {
	rodTemp := filepath.Join(os.TempDir(), "rod", "user-data")
	entries, err := os.ReadDir(rodTemp)
	if err != nil {
		return
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

// sendKeyThroughCDP отправляет клавишу через CDP
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
