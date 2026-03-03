package main

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// HenryClient gerencia a conexão com uma catraca Henry 7x real
type HenryClient struct {
	IP        string
	Port      int
	conn      net.Conn
	mu        sync.Mutex
	connected bool
	listening bool
	OnEvent   func(event HenryEvent) // callback para eventos
}

// HenryEvent representa um evento recebido da catraca
type HenryEvent struct {
	Index      string `json:"index"`
	Command    string `json:"command"`
	Version    string `json:"version"`
	Data       string `json:"data"`
	CardID     string `json:"card_id,omitempty"`
	RawHex     string `json:"raw_hex,omitempty"`
	WorkerName string `json:"worker_name,omitempty"`
}

// CatracaStatus representa o status de conexão com a catraca
type CatracaStatus struct {
	Connected bool   `json:"connected"`
	IP        string `json:"ip"`
	Port      int    `json:"port"`
	Listening bool   `json:"listening"`
}

// Variável global do cliente
var henryClient *HenryClient

// NewHenryClient cria uma nova instância do cliente
func NewHenryClient(ip string, port int) *HenryClient {
	return &HenryClient{
		IP:   ip,
		Port: port,
	}
}

// Connect estabelece conexão TCP com a catraca
func (h *HenryClient) Connect() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.connected && h.conn != nil {
		return nil // já conectado
	}

	addr := fmt.Sprintf("%s:%d", h.IP, h.Port)
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		h.connected = false
		return fmt.Errorf("falha ao conectar em %s: %v", addr, err)
	}

	h.conn = conn
	h.connected = true
	fmt.Printf("🔗 [HENRY CLIENT] Conectado à catraca em %s\n", addr)
	return nil
}

// Disconnect fecha a conexão
func (h *HenryClient) Disconnect() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.listening = false
	if h.conn != nil {
		h.conn.Close()
		h.conn = nil
	}
	h.connected = false
	fmt.Println("🔌 [HENRY CLIENT] Desconectado da catraca")
}

// Status retorna o status atual da conexão
func (h *HenryClient) Status() CatracaStatus {
	return CatracaStatus{
		Connected: h.connected,
		IP:        h.IP,
		Port:      h.Port,
		Listening: h.listening,
	}
}

// Listen escuta eventos da catraca (bloqueante — rodar em goroutine)
func (h *HenryClient) Listen() {
	h.listening = true
	fmt.Println("👂 [HENRY CLIENT] Escutando eventos da catraca...")

	buf := make([]byte, 1024)
	for h.listening {
		if err := h.Connect(); err != nil {
			fmt.Printf("⚠️  [HENRY CLIENT] Reconectando em 5s: %v\n", err)
			time.Sleep(5 * time.Second)
			continue
		}

		h.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		n, err := h.conn.Read(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue // timeout normal, tenta de novo
			}
			fmt.Printf("⚠️  [HENRY CLIENT] Erro de leitura: %v\n", err)
			h.mu.Lock()
			h.connected = false
			h.conn = nil
			h.mu.Unlock()
			time.Sleep(2 * time.Second)
			continue
		}

		data := buf[:n]
		event := h.parseResponse(data)
		if event != nil {
			fmt.Printf("📨 [HENRY CLIENT] Evento: Cmd=%s Data=%s\n", event.Command, event.Data)

			// Se for evento de passagem de cartão (REON), processar automaticamente
			if event.Command == "REON" || event.Command == "EMSG" {
				h.processCardEvent(event)
			}

			if h.OnEvent != nil {
				h.OnEvent(*event)
			}
		}
	}
}

// parseResponse interpreta uma resposta da catraca no protocolo Henry
func (h *HenryClient) parseResponse(data []byte) *HenryEvent {
	if len(data) < 5 {
		return nil
	}
	if data[0] != STX || data[len(data)-1] != ETX {
		return nil
	}

	// STX | Size | 0x00 | Index(2) | payload... | Checksum | ETX
	event := &HenryEvent{
		RawHex: fmt.Sprintf("%x", data),
	}

	if len(data) >= 6 {
		event.Index = string(data[3:5])
	}

	// Extrair payload entre index e checksum
	if len(data) > 7 {
		payload := string(data[5 : len(data)-2])

		// Remover '+' inicial antes de fazer split (mesmo fix do simulador)
		payload = strings.TrimLeft(payload, "+")
		parts := strings.SplitN(payload, "+", 3)
		if len(parts) >= 1 {
			event.Command = parts[0]
		}
		if len(parts) >= 2 {
			event.Version = parts[1]
		}
		if len(parts) >= 3 {
			event.Data = parts[2]
		}

		// Extrair matrícula do campo Data
		dataParts := strings.Split(event.Data, "]")
		if len(dataParts) >= 1 {
			event.CardID = dataParts[0]
		}
	}

	return event
}

// processCardEvent processa o evento de passagem de cartão
func (h *HenryClient) processCardEvent(event *HenryEvent) {
	if event.CardID == "" {
		return
	}

	worker, appliedType, err := LogAttendance(event.CardID, "AUTO")
	if err != nil {
		// Cartão não encontrado — bloquear
		fmt.Printf("🚫 [HENRY CLIENT] Cartão %s não encontrado: %v\n", event.CardID, err)
		h.ImpedirEntrada(event.Index, "ACESSO NEGADO")
	} else {
		// Cartão encontrado — liberar
		fmt.Printf("✅ [HENRY CLIENT] %s: %s (%s)\n", appliedType, worker.Name, event.CardID)
		event.WorkerName = worker.Name
		if appliedType == "ENTRADA" {
			h.PermitirEntrada(event.Index, fmt.Sprintf("ENTRADA %s", worker.Name))
		} else {
			h.PermitirEntrada(event.Index, fmt.Sprintf("ATE LOGO %s", worker.Name))
		}
	}
}

// --- Comandos para enviar à catraca ---

// Send envia um comando genérico à catraca
func (h *HenryClient) Send(index, payload string) error {
	if err := h.Connect(); err != nil {
		return err
	}

	msg := buildHenryMessage(index, payload)
	h.mu.Lock()
	defer h.mu.Unlock()

	_, err := h.conn.Write(msg)
	if err != nil {
		h.connected = false
		return fmt.Errorf("erro ao enviar: %v", err)
	}
	return nil
}

// PermitirEntrada libera a catraca após um evento de passagem
func (h *HenryClient) PermitirEntrada(index, mensagem string) error {
	// REON + 00 + 6] tempo_liberacao ] mensagem ] 2 (liberado)
	payload := fmt.Sprintf("REON+00+6]40]%s]2", mensagem)
	return h.Send(index, payload)
}

// ImpedirEntrada bloqueia a catraca após um evento de passagem
func (h *HenryClient) ImpedirEntrada(index, mensagem string) error {
	// REON + 00 + 30] tempo_liberacao ] mensagem ] 1 (bloqueado)
	payload := fmt.Sprintf("REON+00+30]40]%s]1", mensagem)
	return h.Send(index, payload)
}

// LiberarEntrada libera a catraca independente de evento (pré-escuta)
func (h *HenryClient) LiberarEntrada(mensagem string) error {
	payload := fmt.Sprintf("REON+00+4]40]%s]}1", mensagem)
	return h.Send("00", payload)
}

// CadastrarDigital envia o comando para a catraca entrar em modo de cadastro biométrico
func (h *HenryClient) CadastrarDigital(cardID string) error {
	if err := h.Connect(); err != nil {
		return err
	}

	// Verificar se o aluno existe no banco (busca direta por card_id)
	var exists int
	err := db.QueryRow("SELECT COUNT(*) FROM workers WHERE card_id = ?", cardID).Scan(&exists)
	if err != nil || exists == 0 {
		return fmt.Errorf("aluno com matrícula '%s' não encontrado no sistema", cardID)
	}

	// Comando para cadastro biométrico:
	// Envia matrícula para a catraca iniciar captura
	// O formato exato pode variar dependendo do firmware, mas geralmente:
	// EMSG + 00 + matricula ] 1 (modo cadastro)
	payload := fmt.Sprintf("EMSG+00+%s]1", cardID)
	err = h.Send("00", payload)
	if err != nil {
		return err
	}

	fmt.Printf("🪪  [HENRY CLIENT] Cadastro de digital iniciado para matrícula: %s\n", cardID)
	fmt.Println("👆 [HENRY CLIENT] Aguardando aluno colocar o dedo na catraca...")
	return nil
}

// SetDateTime sincroniza o relógio da catraca com o horário local
func (h *HenryClient) SetDateTime() error {
	now := time.Now()
	dateStr := now.Format("02/01/2006 15:04:05")
	payload := fmt.Sprintf("EH+00+%s]00/00/00]00/00/00", dateStr)
	return h.Send("00", payload)
}

// GetConfig lê uma configuração da catraca
func (h *HenryClient) GetConfig(param string) error {
	payload := fmt.Sprintf("RC+00+%s", param)
	return h.Send("00", payload)
}
