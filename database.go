package main

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var db *sql.DB

func InitDB() {
	var err error
	db, err = sql.Open("sqlite", "./catraca.db")
	if err != nil {
		log.Fatal(err)
	}

	// Habilitar WAL mode para melhor performance e evitar locks
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA busy_timeout=5000")

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

	_, err = db.Exec(createTables)
	if err != nil {
		log.Fatal("Erro ao criar tabelas:", err)
	}

	fmt.Println("✅ Banco de dados SQLite inicializado com sucesso.")
}

// --- Workers (Alunos) CRUD ---

func RegisterWorker(cardID, name, job string) error {
	if cardID == "" || name == "" {
		return fmt.Errorf("card_id e name são obrigatórios")
	}
	_, err := db.Exec("INSERT INTO workers (card_id, name, job_title) VALUES (?, ?, ?)", cardID, name, job)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return fmt.Errorf("matrícula '%s' já está cadastrada", cardID)
		}
		return err
	}
	return nil
}

func GetAllWorkers() ([]Worker, error) {
	rows, err := db.Query("SELECT id, card_id, name, COALESCE(job_title, '') FROM workers ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	workers := make([]Worker, 0) // Garante que retorna [] ao invés de null
	for rows.Next() {
		var w Worker
		if err := rows.Scan(&w.ID, &w.CardID, &w.Name, &w.JobTitle); err != nil {
			return nil, err
		}
		workers = append(workers, w)
	}
	return workers, nil
}

func DeleteWorker(id int) error {
	result, err := db.Exec("DELETE FROM workers WHERE id = ?", id)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("aluno não encontrado")
	}
	return nil
}

// --- Attendance (Ponto) ---

// GetLastAttendanceType retorna o último tipo de ponto do aluno.
// Se o último registro for há mais de 10 horas, retorna "" (força ENTRADA).
func GetLastAttendanceType(workerID int) string {
	var lastType string
	var tsStr string
	err := db.QueryRow(`
		SELECT type, timestamp FROM attendance 
		WHERE worker_id = ? 
		ORDER BY timestamp DESC 
		LIMIT 1
	`, workerID).Scan(&lastType, &tsStr)
	if err != nil {
		return "" // sem registro → ENTRADA
	}

	// Verificar se o último registro foi há mais de 10 horas
	lastTime, err := time.ParseInLocation("2006-01-02 15:04:05", tsStr, time.Local)
	if err != nil {
		return "" // erro no parse → ENTRADA por segurança
	}

	if time.Since(lastTime) > 10*time.Hour {
		return "" // expirado → forçar ENTRADA
	}

	return lastType
}

func LogAttendance(cardID, logType string) (*Worker, string, error) {
	// Buscar aluno
	var worker Worker
	err := db.QueryRow("SELECT id, card_id, name, COALESCE(job_title, '') FROM workers WHERE card_id = ?", cardID).
		Scan(&worker.ID, &worker.CardID, &worker.Name, &worker.JobTitle)
	if err != nil {
		return nil, "", fmt.Errorf("aluno com matrícula '%s' não encontrado", cardID)
	}

	// Lógica de toggle automático
	if logType == "" || logType == "AUTO" {
		last := GetLastAttendanceType(worker.ID)
		if last == "ENTRADA" {
			logType = "SAIDA"
		} else {
			logType = "ENTRADA"
		}
	}

	// Validar tipo final
	if logType != "ENTRADA" && logType != "SAIDA" {
		return nil, "", fmt.Errorf("tipo inválido: use ENTRADA, SAIDA ou AUTO")
	}

	_, err = db.Exec("INSERT INTO attendance (worker_id, type) VALUES (?, ?)", worker.ID, logType)
	if err != nil {
		return nil, "", err
	}

	return &worker, logType, nil
}

func GetRecentLogs(limit int) ([]AttendanceRecord, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	rows, err := db.Query(`
		SELECT a.id, a.worker_id, w.card_id, w.name, COALESCE(w.job_title, ''), a.timestamp, a.type 
		FROM attendance a 
		JOIN workers w ON a.worker_id = w.id 
		ORDER BY a.timestamp DESC 
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	logs := []AttendanceRecord{} // Garante [] ao invés de null no JSON
	for rows.Next() {
		var r AttendanceRecord
		var w Worker
		var tsStr string
		err := rows.Scan(&r.ID, &r.WorkerID, &w.CardID, &w.Name, &w.JobTitle, &tsStr, &r.Type)
		if err != nil {
			return logs, err
		}

		// Parse timestamp como hora local
		r.Timestamp, _ = time.ParseInLocation("2006-01-02 15:04:05", tsStr, time.Local)
		r.Worker = &w
		logs = append(logs, r)
	}
	return logs, nil
}

func GetLogsByDate(dateStr string) ([]AttendanceRecord, error) {
	rows, err := db.Query(`
		SELECT a.id, a.worker_id, w.card_id, w.name, COALESCE(w.job_title, ''), a.timestamp, a.type 
		FROM attendance a 
		JOIN workers w ON a.worker_id = w.id 
		WHERE date(a.timestamp) = ?
		ORDER BY a.timestamp DESC
	`, dateStr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	logs := make([]AttendanceRecord, 0)
	for rows.Next() {
		var r AttendanceRecord
		var w Worker
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

// --- Settings ---

func GetSetting(key string) string {
	var value string
	err := db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&value)
	if err != nil {
		return ""
	}
	return value
}

func SetSetting(key, value string) error {
	_, err := db.Exec("INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = ?", key, value, value)
	return err
}

// --- Import CSV ---

func ImportWorkersCSV(csvData string) (int, int, error) {
	lines := strings.Split(strings.TrimSpace(csvData), "\n")
	imported := 0
	skipped := 0

	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Pular cabeçalho
		if i == 0 {
			lower := strings.ToLower(line)
			if strings.Contains(lower, "matri") || strings.Contains(lower, "nome") || strings.Contains(lower, "card") {
				continue
			}
		}

		// Aceitar ; ou , como separador
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

		// Usar INSERT OR IGNORE para não travar em duplicatas durante import
		_, err := db.Exec("INSERT OR IGNORE INTO workers (card_id, name, job_title) VALUES (?, ?, ?)", cardID, name, job)
		if err != nil {
			skipped++
			continue
		}
		imported++
	}

	return imported, skipped, nil
}

// --- Export ---

func GetLogsByDateRange(startDate, endDate string) ([]AttendanceRecord, error) {
	rows, err := db.Query(`
		SELECT a.id, a.worker_id, w.card_id, w.name, COALESCE(w.job_title, ''), a.timestamp, a.type
		FROM attendance a
		JOIN workers w ON a.worker_id = w.id
		WHERE date(a.timestamp) BETWEEN ? AND ?
		ORDER BY a.timestamp DESC
	`, startDate, endDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	logs := []AttendanceRecord{}
	for rows.Next() {
		var r AttendanceRecord
		var w Worker
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
