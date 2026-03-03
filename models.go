package main

import "time"

// Worker representa um aluno no sistema
type Worker struct {
	ID       int    `json:"id"`
	CardID   string `json:"card_id"`
	Name     string `json:"name"`
	JobTitle string `json:"job_title"`
}

// AttendanceRecord representa um registro de ponto
type AttendanceRecord struct {
	ID        int       `json:"id"`
	WorkerID  int       `json:"worker_id"`
	Worker    *Worker   `json:"worker,omitempty"`
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"` // "ENTRADA" ou "SAIDA"
}
