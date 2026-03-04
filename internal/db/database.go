package db

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/user/catraca/internal/models"
	_ "modernc.org/sqlite"
)

var DB *sql.DB

func InitDB() {
	var err error
	DB, err = sql.Open("sqlite", "./catraca.db")
	if err != nil {
		log.Fatal(err)
	}

	DB.Exec("PRAGMA journal_mode=WAL")
	DB.Exec("PRAGMA busy_timeout=5000")

	createTables := `
	CREATE TABLE IF NOT EXISTS workers (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		card_id TEXT UNIQUE NOT NULL,
		name TEXT NOT NULL,
		job_title TEXT,
		created_at TEXT DEFAULT (datetime('now','localtime'))
	);

	CREATE TABLE IF NOT EXISTS attendance (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		worker_id INTEGER NOT NULL,
		timestamp TEXT DEFAULT (datetime('now','localtime')),
		type TEXT NOT NULL CHECK(type IN ('ENTRADA', 'SAIDA')),
		FOREIGN KEY (worker_id) REFERENCES workers(id)
	);

	CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_attendance_timestamp ON attendance(timestamp DESC);
	CREATE INDEX IF NOT EXISTS idx_workers_card ON workers(card_id);
	`

	_, err = DB.Exec(createTables)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Banco de dados SQLite inicializado com sucesso.")
}

func RegisterWorker(cardID, name, job string) error {
	if cardID == "" || name == "" {
		return fmt.Errorf("card_id e name sao obrigatorios")
	}
	_, err := DB.Exec("INSERT INTO workers (card_id, name, job_title) VALUES (?, ?, ?)", cardID, name, job)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return fmt.Errorf("matricula '%s' ja esta cadastrada", cardID)
		}
		return err
	}
	return nil
}

func GetAllWorkers() ([]models.Worker, error) {
	rows, err := DB.Query("SELECT id, card_id, name, COALESCE(job_title, '') FROM workers ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	workers := make([]models.Worker, 0)
	for rows.Next() {
		var w models.Worker
		if err := rows.Scan(&w.ID, &w.CardID, &w.Name, &w.JobTitle); err != nil {
			return nil, err
		}
		workers = append(workers, w)
	}
	return workers, nil
}

func DeleteWorker(id int) error {
	result, err := DB.Exec("DELETE FROM workers WHERE id = ?", id)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("aluno nao encontrado")
	}
	return nil
}

func GetLastAttendanceType(workerID int) string {
	var lastType string
	var tsStr string
	err := DB.QueryRow("SELECT type, timestamp FROM attendance WHERE worker_id = ? ORDER BY timestamp DESC LIMIT 1", workerID).Scan(&lastType, &tsStr)
	if err != nil {
		return ""
	}

	lastTime, err := time.ParseInLocation("2006-01-02 15:04:05", tsStr, time.Local)
	if err != nil {
		return ""
	}

	if time.Since(lastTime) > 10*time.Hour {
		return ""
	}

	return lastType
}

func LogAttendance(cardID, logType string) (*models.Worker, string, error) {
	var worker models.Worker
	err := DB.QueryRow("SELECT id, card_id, name, COALESCE(job_title, '') FROM workers WHERE card_id = ?", cardID).
		Scan(&worker.ID, &worker.CardID, &worker.Name, &worker.JobTitle)
	if err != nil {
		return nil, "", fmt.Errorf("aluno com matricula '%s' nao encontrado", cardID)
	}

	if logType == "" || logType == "AUTO" {
		last := GetLastAttendanceType(worker.ID)
		if last == "ENTRADA" {
			logType = "SAIDA"
		} else {
			logType = "ENTRADA"
		}
	}

	if logType != "ENTRADA" && logType != "SAIDA" {
		return nil, "", fmt.Errorf("tipo invalido")
	}

	_, err = DB.Exec("INSERT INTO attendance (worker_id, type) VALUES (?, ?)", worker.ID, logType)
	if err != nil {
		return nil, "", err
	}

	return &worker, logType, nil
}

func GetRecentLogs(limit int) ([]models.AttendanceRecord, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	rows, err := DB.Query("SELECT a.id, a.worker_id, w.card_id, w.name, COALESCE(w.job_title, ''), a.timestamp, a.type FROM attendance a JOIN workers w ON a.worker_id = w.id ORDER BY a.timestamp DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	logs := []models.AttendanceRecord{}
	for rows.Next() {
		var r models.AttendanceRecord
		var w models.Worker
		var tsStr string
		err := rows.Scan(&r.ID, &r.WorkerID, &w.CardID, &w.Name, &w.JobTitle, &tsStr, &r.Type)
		if err != nil {
			return logs, err
		}

		r.Timestamp, _ = time.ParseInLocation("2006-01-02 15:04:05", tsStr, time.Local)
		r.Worker = &w
		logs = append(logs, r)
	}
	return logs, nil
}

func GetLogsByDate(dateStr string) ([]models.AttendanceRecord, error) {
	rows, err := DB.Query("SELECT a.id, a.worker_id, w.card_id, w.name, COALESCE(w.job_title, ''), a.timestamp, a.type FROM attendance a JOIN workers w ON a.worker_id = w.id WHERE date(a.timestamp) = ? ORDER BY a.timestamp DESC", dateStr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	logs := make([]models.AttendanceRecord, 0)
	for rows.Next() {
		var r models.AttendanceRecord
		var w models.Worker
		var tsStr string
		err := rows.Scan(&r.ID, &r.WorkerID, &w.CardID, &w.Name, &w.JobTitle, &tsStr, &r.Type)
		if err != nil {
			return nil, err
		}
		r.Timestamp, _ = time.ParseInLocation("2006-01-02 15:04:05", tsStr, time.Local)
		r.Worker = &w
		logs = append(logs, r)
	}
	return logs, nil
}

func GetSetting(key string) string {
	var value string
	err := DB.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&value)
	if err != nil {
		return ""
	}
	return value
}

func SetSetting(key, value string) error {
	_, err := DB.Exec("INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = ?", key, value, value)
	return err
}

func ImportWorkersCSV(csvData string) (int, int, error) {
	lines := strings.Split(strings.TrimSpace(csvData), "\n")
	imported := 0
	skipped := 0

	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if i == 0 {
			lower := strings.ToLower(line)
			if strings.Contains(lower, "matri") || strings.Contains(lower, "nome") || strings.Contains(lower, "card") {
				continue
			}
		}

		var parts []string
		if strings.Contains(line, ";") {
			parts = strings.Split(line, ";")
		} else {
			parts = strings.Split(line, ",")
		}

		if len(parts) < 2 {
			skipped++
			continue
		}

		cardID := strings.TrimSpace(parts[0])
		name := strings.TrimSpace(parts[1])
		job := ""
		if len(parts) >= 3 {
			job = strings.TrimSpace(parts[2])
		}

		if cardID == "" || name == "" {
			skipped++
			continue
		}

		_, err := DB.Exec("INSERT OR IGNORE INTO workers (card_id, name, job_title) VALUES (?, ?, ?)", cardID, name, job)
		if err != nil {
			skipped++
			continue
		}
		imported++
	}

	return imported, skipped, nil
}

func GetLogsByDateRange(startDate, endDate string) ([]models.AttendanceRecord, error) {
	rows, err := DB.Query("SELECT a.id, a.worker_id, w.card_id, w.name, COALESCE(w.job_title, ''), a.timestamp, a.type FROM attendance a JOIN workers w ON a.worker_id = w.id WHERE date(a.timestamp) BETWEEN ? AND ? ORDER BY a.timestamp DESC", startDate, endDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	logs := []models.AttendanceRecord{}
	for rows.Next() {
		var r models.AttendanceRecord
		var w models.Worker
		var tsStr string
		err := rows.Scan(&r.ID, &r.WorkerID, &w.CardID, &w.Name, &w.JobTitle, &tsStr, &r.Type)
		if err != nil {
			return logs, err
		}
		r.Timestamp, _ = time.ParseInLocation("2006-01-02 15:04:05", tsStr, time.Local)
		r.Worker = &w
		logs = append(logs, r)
	}
	return logs, nil
}
