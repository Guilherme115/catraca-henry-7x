package henry

import (
	"bytes"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/user/catraca/internal/db"
)

type HenryClient struct {
	IP        string
	Port      int
	conn      net.Conn
	mu        sync.Mutex
	connected bool
	listening bool
	OnEvent   func(event HenryEvent)
}

type HenryEvent struct {
	Index      string `json:"index"`
	Command    string `json:"command"`
	Version    string `json:"version"`
	Data       string `json:"data"`
	CardID     string `json:"card_id,omitempty"`
	RawHex     string `json:"raw_hex,omitempty"`
	WorkerName string `json:"worker_name,omitempty"`
}

type CatracaStatus struct {
	Connected bool   `json:"connected"`
	IP        string `json:"ip"`
	Port      int    `json:"port"`
	Listening bool   `json:"listening"`
}

func NewHenryClient(ip string, port int) *HenryClient {
	return &HenryClient{
		IP:   ip,
		Port: port,
	}
}

func (h *HenryClient) Connect() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.connected && h.conn != nil {
		return nil
	}

	addr := fmt.Sprintf("%s:%d", h.IP, h.Port)
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		h.connected = false
		return fmt.Errorf("falha ao conectar em %s: %v", addr, err)
	}

	h.conn = conn
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetKeepAlive(true)
		tcpConn.SetKeepAlivePeriod(20 * time.Second)
	}
	h.connected = true
	fmt.Printf("Conectado a catraca em %s\n", addr)
	return nil
}

func (h *HenryClient) Disconnect() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.listening = false
	if h.conn != nil {
		h.conn.Close()
		h.conn = nil
	}
	h.connected = false
	fmt.Println("Desconectado da catraca")
}

func (h *HenryClient) Status() CatracaStatus {
	h.mu.Lock()
	defer h.mu.Unlock()

	return CatracaStatus{
		Connected: h.connected,
		IP:        h.IP,
		Port:      h.Port,
		Listening: h.listening,
	}
}

func (h *HenryClient) IsConnected() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.connected && h.conn != nil
}

func (h *HenryClient) Listen() {
	h.mu.Lock()
	h.listening = true
	h.mu.Unlock()
	fmt.Println("Escutando eventos da catraca...")

	buf := make([]byte, 1024)
	stream := make([]byte, 0, 2048)
	for h.isListening() {
		if err := h.Connect(); err != nil {
			fmt.Printf("Reconectando em 5s: %v\n", err)
			time.Sleep(5 * time.Second)
			continue
		}

		h.mu.Lock()
		conn := h.conn
		h.mu.Unlock()
		if conn == nil {
			time.Sleep(1 * time.Second)
			continue
		}

		conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		n, err := conn.Read(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			fmt.Printf("Erro de leitura: %v\n", err)
			h.resetConnection()
			time.Sleep(2 * time.Second)
			continue
		}

		stream = append(stream, buf[:n]...)
		frames, tail := extractHenryFrames(stream)
		stream = tail

		for _, frame := range frames {
			event := h.parseResponse(frame)
			if event != nil {
				fmt.Printf("Evento: Cmd=%s Data=%s\n", event.Command, event.Data)

				if event.Command == "REON" {
					h.processCardEvent(event)
				}

				if h.OnEvent != nil {
					h.OnEvent(*event)
				}
			}
		}
	}
}

func (h *HenryClient) isListening() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.listening
}

func (h *HenryClient) resetConnection() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conn != nil {
		h.conn.Close()
	}
	h.conn = nil
	h.connected = false
}

func extractHenryFrames(stream []byte) ([][]byte, []byte) {
	frames := make([][]byte, 0)
	cursor := 0

	for {
		start := bytes.IndexByte(stream[cursor:], STX)
		if start == -1 {
			// If no more STX is found, but we haven't processed the whole stream,
			// what remains is garbage. Discard it.
			return frames, nil
		}
		start += cursor

		end := bytes.IndexByte(stream[start+1:], ETX)
		if end == -1 {
			// Incomplete frame. Return what we have from STX onwards.
			return frames, stream[start:]
		}
		end += start + 1

		frame := make([]byte, end-start+1)
		copy(frame, stream[start:end+1])
		frames = append(frames, frame)
		cursor = end + 1

		if cursor >= len(stream) {
			return frames, nil
		}
	}
}

func (h *HenryClient) parseResponse(data []byte) *HenryEvent {
	if len(data) < 5 {
		return nil
	}
	if data[0] != STX || data[len(data)-1] != ETX {
		return nil
	}

	event := &HenryEvent{
		RawHex: fmt.Sprintf("%x", data),
	}

	if len(data) >= 6 {
		event.Index = string(data[3:5])
	}

	if len(data) > 7 {
		payload := string(data[5 : len(data)-2])

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

		dataParts := strings.Split(event.Data, "]")
		if len(dataParts) >= 1 {
			event.CardID = dataParts[0]
		}
	}

	return event
}

func (h *HenryClient) processCardEvent(event *HenryEvent) {
	if event.CardID == "" {
		return
	}

	worker, appliedType, err := db.LogAttendance(event.CardID, "AUTO")
	if err != nil {
		fmt.Printf("Cartao %s nao encontrado: %v\n", event.CardID, err)
		h.ImpedirEntrada(event.Index, "ACESSO NEGADO")
	} else {
		fmt.Printf("%s: %s (%s)\n", appliedType, worker.Name, event.CardID)
		event.WorkerName = worker.Name
		if appliedType == "ENTRADA" {
			h.PermitirEntrada(event.Index, fmt.Sprintf("ENTRADA %s", worker.Name))
		} else {
			h.PermitirEntrada(event.Index, fmt.Sprintf("ATE LOGO %s", worker.Name))
		}
	}
}

func (h *HenryClient) Send(index, payload string) error {
	if err := h.Connect(); err != nil {
		return err
	}

	msg := buildHenryMessage(index, payload)
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.conn == nil {
		h.connected = false
		return fmt.Errorf("conexao indisponivel")
	}

	_, err := h.conn.Write(msg)
	if err != nil {
		h.connected = false
		h.conn.Close()
		h.conn = nil
		return fmt.Errorf("erro ao enviar: %v", err)
	}
	return nil
}

func (h *HenryClient) PermitirEntrada(index, mensagem string) error {
	payload := fmt.Sprintf("REON+00+6]40]%s]2", mensagem)
	return h.Send(index, payload)
}

func (h *HenryClient) ImpedirEntrada(index, mensagem string) error {
	payload := fmt.Sprintf("REON+00+30]40]%s]1", mensagem)
	return h.Send(index, payload)
}

func (h *HenryClient) LiberarEntrada(mensagem string) error {
	payload := fmt.Sprintf("REON+00+4]40]%s]1", mensagem)
	return h.Send("00", payload)
}

func (h *HenryClient) CadastrarDigital(cardID string) error {
	if err := h.Connect(); err != nil {
		return err
	}

	var exists int
	err := db.DB.QueryRow("SELECT COUNT(*) FROM workers WHERE card_id = ?", cardID).Scan(&exists)
	if err != nil || exists == 0 {
		return fmt.Errorf("aluno com matricula '%s' nao encontrado no sistema", cardID)
	}

	payload := fmt.Sprintf("EMSG+00+%s]1", cardID)
	err = h.Send("00", payload)
	if err != nil {
		return err
	}

	fmt.Printf("Cadastro de digital iniciado para matricula: %s\n", cardID)
	fmt.Println("Aguardando aluno colocar o dedo na catraca...")
	return nil
}

func (h *HenryClient) BipTeste(msg string) error {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		msg = "TESTE COMUNICACAO"
	}
	msg = strings.ReplaceAll(msg, "+", " ")
	payload := fmt.Sprintf("EMSG+00+%s]1", msg)
	return h.Send("00", payload)
}

func (h *HenryClient) SetDateTime() error {
	now := time.Now()
	dateStr := now.Format("02/01/2006 15:04:05")
	payload := fmt.Sprintf("EH+00+%s]00/00/00]00/00/00", dateStr)
	return h.Send("00", payload)
}

func (h *HenryClient) GetConfig(param string) error {
	payload := fmt.Sprintf("RC+00+%s", param)
	return h.Send("00", payload)
}
