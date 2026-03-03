package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func main() {
	// Inicializar Banco de Dados
	InitDB()

	// Inicializar Cliente Henry (sem conectar ainda — o usuário configura o IP)
	henryClient = NewHenryClient("", 3000)

	// Iniciar simulador apenas quando explicitamente habilitado.
	if os.Getenv("HENRY_SIMULATOR") == "1" {
		go StartHenrySimulator()
		fmt.Println("🧪 Simulador Henry habilitado (HENRY_SIMULATOR=1)")
	}

	// Mux customizado
	mux := http.NewServeMux()

	// Arquivos Estáticos (Frontend)
	fs := http.FileServer(http.Dir("./static"))
	mux.Handle("/", fs)

	// API Routes — Ponto
	mux.HandleFunc("/api/logs", handleGetLogs)
	mux.HandleFunc("/api/punch", handlePunch)

	// API Routes — Alunos
	mux.HandleFunc("/api/workers", handleWorkers)
	mux.HandleFunc("/api/workers/", handleWorkerDelete)
	mux.HandleFunc("/api/workers/import", handleWorkersImport)

	// API Routes — Exportação
	mux.HandleFunc("/api/export", handleExport)

	// API Routes — Catraca Real
	mux.HandleFunc("/api/catraca/status", handleCatracaStatus)
	mux.HandleFunc("/api/catraca/connect", handleCatracaConnect)
	mux.HandleFunc("/api/catraca/disconnect", handleCatracaDisconnect)
	mux.HandleFunc("/api/catraca/enroll", handleCatracaEnroll)
	mux.HandleFunc("/api/catraca/sync-clock", handleCatracaSyncClock)
	mux.HandleFunc("/api/catraca/release", handleCatracaRelease)

	server := &http.Server{
		Addr:         ":8082",
		Handler:      corsMiddleware(mux),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful Shutdown
	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit
		fmt.Println("\n🛑 Encerrando servidor...")
		if henryClient != nil {
			henryClient.Disconnect()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}()

	fmt.Println("🚀 Servidor iniciado em http://localhost:8082")
	fmt.Println("📋 Para conectar numa catraca Henry 7x real, use o painel 'Catraca' na interface web")
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal(err)
	}
	fmt.Println("👋 Servidor encerrado com sucesso.")
}

// --- Middleware CORS ---
func corsMiddleware(next http.Handler) http.Handler {
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

// --- Helpers ---
func jsonResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, status int, message string) {
	jsonResponse(w, status, map[string]string{"error": message})
}

// --- Handlers de Ponto ---
func handleGetLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "Método não permitido")
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

	var logs []AttendanceRecord
	var err error

	if dateFilter != "" {
		logs, err = GetLogsByDate(dateFilter)
	} else {
		logs, err = GetRecentLogs(limit)
	}

	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, logs)
}

func handlePunch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Método não permitido")
		return
	}

	var data struct {
		CardID string `json:"card_id"`
		Type   string `json:"type"`
	}

	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		jsonError(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	data.CardID = strings.TrimSpace(data.CardID)
	data.Type = strings.ToUpper(strings.TrimSpace(data.Type))

	if data.CardID == "" {
		jsonError(w, http.StatusBadRequest, "Matrícula é obrigatória")
		return
	}

	worker, appliedType, err := LogAttendance(data.CardID, data.Type)
	if err != nil {
		jsonError(w, http.StatusNotFound, err.Error())
		return
	}

	fmt.Printf("📋 [PONTO] %s → %s (%s)\n", appliedType, worker.Name, data.CardID)

	jsonResponse(w, http.StatusOK, map[string]string{
		"message":      fmt.Sprintf("%s registrado para %s", appliedType, worker.Name),
		"worker":       worker.Name,
		"applied_type": appliedType,
	})
}

// --- Handlers de Alunos ---
func handleWorkers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		workers, err := GetAllWorkers()
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
			jsonError(w, http.StatusBadRequest, "JSON inválido")
			return
		}

		data.CardID = strings.TrimSpace(data.CardID)
		data.Name = strings.TrimSpace(data.Name)

		if data.CardID == "" || data.Name == "" {
			jsonError(w, http.StatusBadRequest, "card_id e name são obrigatórios")
			return
		}

		if err := RegisterWorker(data.CardID, data.Name, data.JobTitle); err != nil {
			jsonError(w, http.StatusConflict, err.Error())
			return
		}

		fmt.Printf("👤 [ALUNO] Cadastrado: %s (%s)\n", data.Name, data.CardID)
		jsonResponse(w, http.StatusCreated, map[string]string{"message": "Aluno cadastrado com sucesso"})

	default:
		jsonError(w, http.StatusMethodNotAllowed, "Método não permitido")
	}
}

func handleWorkerDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		jsonError(w, http.StatusMethodNotAllowed, "Método não permitido")
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 {
		jsonError(w, http.StatusBadRequest, "ID inválido")
		return
	}

	id, err := strconv.Atoi(parts[3])
	if err != nil {
		jsonError(w, http.StatusBadRequest, "ID deve ser numérico")
		return
	}

	if err := DeleteWorker(id); err != nil {
		jsonError(w, http.StatusNotFound, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, map[string]string{"message": "Aluno removido"})
}

// --- Handlers da Catraca Real ---

func handleCatracaStatus(w http.ResponseWriter, r *http.Request) {
	if henryClient == nil {
		jsonResponse(w, http.StatusOK, CatracaStatus{Connected: false})
		return
	}
	jsonResponse(w, http.StatusOK, henryClient.Status())
}

func handleCatracaConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Método não permitido")
		return
	}

	var data struct {
		IP   string `json:"ip"`
		Port int    `json:"port"`
	}

	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		jsonError(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	if data.IP == "" {
		jsonError(w, http.StatusBadRequest, "IP é obrigatório")
		return
	}
	if data.Port == 0 {
		data.Port = 3000
	}

	// Desconectar se já estiver conectado
	if henryClient != nil && henryClient.IsConnected() {
		henryClient.Disconnect()
	}

	henryClient = NewHenryClient(data.IP, data.Port)

	// Callback para eventos
	henryClient.OnEvent = func(event HenryEvent) {
		fmt.Printf("📡 [EVENTO] Cmd=%s Card=%s Worker=%s\n", event.Command, event.CardID, event.WorkerName)
	}

	err := henryClient.Connect()
	if err != nil {
		jsonError(w, http.StatusServiceUnavailable, err.Error())
		return
	}

	// Iniciar listener em background
	go henryClient.Listen()

	// Sincronizar relógio
	henryClient.SetDateTime()

	// Salvar IP/Porta para reconexão automática
	SetSetting("catraca_ip", data.IP)
	SetSetting("catraca_port", strconv.Itoa(data.Port))

	fmt.Printf("🔗 [CATRACA] Conectado em %s:%d\n", data.IP, data.Port)
	jsonResponse(w, http.StatusOK, map[string]string{
		"message": fmt.Sprintf("Conectado à catraca em %s:%d", data.IP, data.Port),
	})
}

func handleCatracaDisconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Método não permitido")
		return
	}

	if henryClient != nil {
		henryClient.Disconnect()
	}
	jsonResponse(w, http.StatusOK, map[string]string{"message": "Desconectado"})
}

func handleCatracaEnroll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Método não permitido")
		return
	}

	var data struct {
		CardID string `json:"card_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		jsonError(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	if data.CardID == "" {
		jsonError(w, http.StatusBadRequest, "Matrícula é obrigatória")
		return
	}

	if henryClient == nil || !henryClient.IsConnected() {
		jsonError(w, http.StatusServiceUnavailable, "Catraca não conectada. Configure o IP primeiro.")
		return
	}

	err := henryClient.CadastrarDigital(data.CardID)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, map[string]string{
		"message": fmt.Sprintf("Cadastro de digital iniciado para matrícula %s. Peça ao aluno para colocar o dedo na catraca.", data.CardID),
	})
}

func handleCatracaSyncClock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Método não permitido")
		return
	}

	if henryClient == nil || !henryClient.IsConnected() {
		jsonError(w, http.StatusServiceUnavailable, "Catraca não conectada")
		return
	}

	err := henryClient.SetDateTime()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, map[string]string{"message": "Relógio sincronizado"})
}

func handleCatracaRelease(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Método não permitido")
		return
	}

	if henryClient == nil || !henryClient.IsConnected() {
		jsonError(w, http.StatusServiceUnavailable, "Catraca não conectada")
		return
	}

	err := henryClient.LiberarEntrada("LIBERADO")
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, map[string]string{"message": "Catraca liberada"})
}

// --- Import CSV ---

func handleWorkersImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Método não permitido")
		return
	}

	var data struct {
		CSV string `json:"csv"`
	}

	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		jsonError(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	if data.CSV == "" {
		jsonError(w, http.StatusBadRequest, "CSV vazio")
		return
	}

	imported, skipped, err := ImportWorkersCSV(data.CSV)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	fmt.Printf("📁 [IMPORT] %d importados, %d ignorados\n", imported, skipped)
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"message":  fmt.Sprintf("%d alunos importados, %d ignorados (duplicados ou inválidos)", imported, skipped),
		"imported": imported,
		"skipped":  skipped,
	})
}

// --- Export CSV ---

func handleExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "Método não permitido")
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

	logs, err := GetLogsByDateRange(startDate, endDate)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	format := r.URL.Query().Get("format")
	if format == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=relatorio_%s_%s.csv", startDate, endDate))
		w.Write([]byte("\xEF\xBB\xBF")) // BOM para Excel
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
