package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/user/catraca/internal/db"
	"github.com/user/catraca/internal/henry"
	"github.com/user/catraca/internal/models"
)

var HC *henry.HenryClient

func RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/logs", handleGetLogs)
	mux.HandleFunc("/api/punch", handlePunch)
	mux.HandleFunc("/api/workers", handleWorkers)
	mux.HandleFunc("/api/workers/", handleWorkerDelete)
	mux.HandleFunc("/api/workers/import", handleWorkersImport)
	mux.HandleFunc("/api/export", handleExport)

	mux.HandleFunc("/api/catraca/status", handleCatracaStatus)
	mux.HandleFunc("/api/catraca/connect", handleCatracaConnect)
	mux.HandleFunc("/api/catraca/disconnect", handleCatracaDisconnect)
	mux.HandleFunc("/api/catraca/enroll", handleCatracaEnroll)
	mux.HandleFunc("/api/catraca/sync-clock", handleCatracaSyncClock)
	mux.HandleFunc("/api/catraca/release", handleCatracaRelease)
	mux.HandleFunc("/api/catraca/beep", handleCatracaBeep)
}

func CorsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func jsonResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, status int, message string) {
	jsonResponse(w, status, map[string]string{"error": message})
}

func handleGetLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "Metodo nao permitido")
		return
	}

	limitStr := r.URL.Query().Get("limit")
	limit := 20
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			limit = l
		}
	}

	dateFilter := r.URL.Query().Get("date")

	var logs []models.AttendanceRecord
	var err error

	if dateFilter != "" {
		logs, err = db.GetLogsByDate(dateFilter)
	} else {
		logs, err = db.GetRecentLogs(limit)
	}

	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, logs)
}

func handlePunch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Metodo nao permitido")
		return
	}

	var data struct {
		CardID string `json:"card_id"`
		Type   string `json:"type"`
	}

	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		jsonError(w, http.StatusBadRequest, "JSON invalido")
		return
	}

	data.CardID = strings.TrimSpace(data.CardID)
	data.Type = strings.ToUpper(strings.TrimSpace(data.Type))

	if data.CardID == "" {
		jsonError(w, http.StatusBadRequest, "Matricula e obrigatoria")
		return
	}

	worker, appliedType, err := db.LogAttendance(data.CardID, data.Type)
	if err != nil {
		jsonError(w, http.StatusNotFound, err.Error())
		return
	}

	fmt.Printf("[PONTO] %s -> %s (%s)\n", appliedType, worker.Name, data.CardID)

	jsonResponse(w, http.StatusOK, map[string]string{
		"message":      fmt.Sprintf("%s registrado para %s", appliedType, worker.Name),
		"worker":       worker.Name,
		"applied_type": appliedType,
	})
}

func handleWorkers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		workers, err := db.GetAllWorkers()
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		jsonResponse(w, http.StatusOK, workers)

	case http.MethodPost:
		var data struct {
			CardID   string `json:"card_id"`
			Name     string `json:"name"`
			JobTitle string `json:"job_title"`
		}
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			jsonError(w, http.StatusBadRequest, "JSON invalido")
			return
		}

		data.CardID = strings.TrimSpace(data.CardID)
		data.Name = strings.TrimSpace(data.Name)

		if data.CardID == "" || data.Name == "" {
			jsonError(w, http.StatusBadRequest, "card_id e name sao obrigatorios")
			return
		}

		if err := db.RegisterWorker(data.CardID, data.Name, data.JobTitle); err != nil {
			jsonError(w, http.StatusConflict, err.Error())
			return
		}

		fmt.Printf("[ALUNO] Cadastrado: %s (%s)\n", data.Name, data.CardID)
		jsonResponse(w, http.StatusCreated, map[string]string{"message": "Aluno cadastrado com sucesso"})

	default:
		jsonError(w, http.StatusMethodNotAllowed, "Metodo nao permitido")
	}
}

func handleWorkerDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		jsonError(w, http.StatusMethodNotAllowed, "Metodo nao permitido")
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 {
		jsonError(w, http.StatusBadRequest, "ID invalido")
		return
	}

	id, err := strconv.Atoi(parts[3])
	if err != nil {
		jsonError(w, http.StatusBadRequest, "ID deve ser numerico")
		return
	}

	if err := db.DeleteWorker(id); err != nil {
		jsonError(w, http.StatusNotFound, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, map[string]string{"message": "Aluno removido"})
}

func handleCatracaStatus(w http.ResponseWriter, r *http.Request) {
	if HC == nil {
		jsonResponse(w, http.StatusOK, henry.CatracaStatus{Connected: false})
		return
	}
	jsonResponse(w, http.StatusOK, HC.Status())
}

func handleCatracaConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Metodo nao permitido")
		return
	}

	var data struct {
		IP   string `json:"ip"`
		Port int    `json:"port"`
	}

	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		jsonError(w, http.StatusBadRequest, "JSON invalido")
		return
	}

	if data.IP == "" {
		jsonError(w, http.StatusBadRequest, "IP e obrigatorio")
		return
	}
	if data.Port == 0 {
		data.Port = 3000
	}

	if HC != nil && HC.IsConnected() {
		HC.Disconnect()
	}

	HC = henry.NewHenryClient(data.IP, data.Port)

	HC.OnEvent = func(event henry.HenryEvent) {
		fmt.Printf("[EVENTO] Cmd=%s Card=%s Worker=%s\n", event.Command, event.CardID, event.WorkerName)
	}

	err := HC.Connect()
	if err != nil {
		jsonError(w, http.StatusServiceUnavailable, err.Error())
		return
	}

	go HC.Listen()

	db.SetSetting("catraca_ip", data.IP)
	db.SetSetting("catraca_port", strconv.Itoa(data.Port))

	fmt.Printf("[CATRACA] Conectado em %s:%d\n", data.IP, data.Port)
	jsonResponse(w, http.StatusOK, map[string]string{
		"message": fmt.Sprintf("Conectado a catraca em %s:%d", data.IP, data.Port),
	})
}

func handleCatracaDisconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Metodo nao permitido")
		return
	}

	if HC != nil {
		HC.Disconnect()
	}
	jsonResponse(w, http.StatusOK, map[string]string{"message": "Desconectado"})
}

func handleCatracaEnroll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Metodo nao permitido")
		return
	}

	var data struct {
		CardID string `json:"card_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		jsonError(w, http.StatusBadRequest, "JSON invalido")
		return
	}

	if data.CardID == "" {
		jsonError(w, http.StatusBadRequest, "Matricula e obrigatoria")
		return
	}

	if HC == nil || !HC.IsConnected() {
		jsonError(w, http.StatusServiceUnavailable, "Catraca nao conectada. Configure o IP primeiro.")
		return
	}

	err := HC.CadastrarDigital(data.CardID)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, map[string]string{
		"message": fmt.Sprintf("Cadastro de digital iniciado para matricula %s. Peca ao aluno para colocar o dedo na catraca.", data.CardID),
	})
}

func handleCatracaSyncClock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Metodo nao permitido")
		return
	}

	if HC == nil || !HC.IsConnected() {
		jsonError(w, http.StatusServiceUnavailable, "Catraca nao conectada")
		return
	}

	err := HC.SetDateTime()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, map[string]string{"message": "Relogio sincronizado"})
}

func handleCatracaRelease(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Metodo nao permitido")
		return
	}

	if HC == nil || !HC.IsConnected() {
		jsonError(w, http.StatusServiceUnavailable, "Catraca nao conectada")
		return
	}

	err := HC.LiberarEntrada("LIBERADO")
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, map[string]string{"message": "Catraca liberada"})
}

func handleCatracaBeep(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Metodo nao permitido")
		return
	}

	if HC == nil || !HC.IsConnected() {
		jsonError(w, http.StatusServiceUnavailable, "Catraca nao conectada")
		return
	}

	var data struct {
		Message string `json:"message"`
	}
	_ = json.NewDecoder(r.Body).Decode(&data)

	if err := HC.BipTeste(data.Message); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, map[string]string{"message": "Comando de bip enviado para a catraca"})
}

func handleWorkersImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Metodo nao permitido")
		return
	}

	var data struct {
		CSV string `json:"csv"`
	}

	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		jsonError(w, http.StatusBadRequest, "JSON invalido")
		return
	}

	if data.CSV == "" {
		jsonError(w, http.StatusBadRequest, "CSV vazio")
		return
	}

	imported, skipped, err := db.ImportWorkersCSV(data.CSV)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	fmt.Printf("[IMPORT] %d importados, %d ignorados\n", imported, skipped)
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"message":  fmt.Sprintf("%d alunos importados, %d ignorados", imported, skipped),
		"imported": imported,
		"skipped":  skipped,
	})
}

func handleExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "Metodo nao permitido")
		return
	}

	startDate := r.URL.Query().Get("start")
	endDate := r.URL.Query().Get("end")

	if startDate == "" {
		startDate = time.Now().Format("2006-01-02")
	}
	if endDate == "" {
		endDate = startDate
	}

	logs, err := db.GetLogsByDateRange(startDate, endDate)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	format := r.URL.Query().Get("format")
	if format == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=relatorio_%s_%s.csv", startDate, endDate))
		w.Write([]byte("\xEF\xBB\xBF"))
		w.Write([]byte("Matricula;Nome;Turma;Data;Hora;Tipo\n"))
		for _, log := range logs {
			w.Write([]byte(fmt.Sprintf("%s;%s;%s;%s;%s;%s\n",
				log.Worker.CardID,
				log.Worker.Name,
				log.Worker.JobTitle,
				log.Timestamp.Format("02/01/2006"),
				log.Timestamp.Format("15:04:05"),
				log.Type,
			)))
		}
		return
	}

	jsonResponse(w, http.StatusOK, logs)
}
