package main

import (
	"fmt"
	"net"
	"strings"
	"time"
)

// Constantes do Protocolo Henry
const (
	STX = 0x02
	ETX = 0x03
)

// StartHenrySimulator inicia o servidor TCP que emula uma catraca Henry 7x na porta 3000
func StartHenrySimulator() {
	ln, err := net.Listen("tcp", ":3000")
	if err != nil {
		fmt.Println("⚠️  Erro ao iniciar simulador Henry:", err)
		return
	}
	defer ln.Close()
	fmt.Println("🔌 Simulador Henry 7x ouvindo na porta 3000...")

	for {
		conn, err := ln.Accept()
		if err != nil {
			continue
		}
		go handleHenryConnection(conn)
	}
}

func handleHenryConnection(conn net.Conn) {
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))

	buf := make([]byte, 1024)
	remoteAddr := conn.RemoteAddr().String()

	fmt.Printf("\n🔗 [HENRY] Conexão de: %s\n", remoteAddr)

	for {
		n, err := conn.Read(buf)
		if err != nil {
			fmt.Printf("🔌 [HENRY] Desconectado: %s\n", remoteAddr)
			return
		}

		data := buf[:n]
		if len(data) < 2 {
			continue
		}

		if data[0] == STX && data[len(data)-1] == ETX {
			// Parse do protocolo Henry real:
			// STX | Size | 0x00 | Index(2 bytes) | "+" | Comando | "+" | Version | "+" | Data | Checksum | ETX

			// Extrair payload (sem STX, checksum e ETX)
			payload := data[1 : len(data)-2] // Remove STX e os 2 últimos (checksum + ETX)

			// Verificar checksum
			receivedChecksum := data[len(data)-2]
			calculatedChecksum := henryChecksum(data[1 : len(data)-2])

			checksumOK := receivedChecksum == calculatedChecksum

			if len(payload) >= 4 {
				// Size está no byte 0 do payload
				// Null byte no byte 1
				// Index nos bytes 2-3
				index := string(payload[2:4])

				// O resto após o index é separado por '+'
				// Remover o '+' inicial antes de fazer split
				rest := strings.TrimLeft(string(payload[4:]), "+")
				parts := strings.SplitN(rest, "+", 3)

				command := ""
				version := ""
				msgData := ""

				if len(parts) >= 1 {
					command = parts[0]
				}
				if len(parts) >= 2 {
					version = parts[1]
				}
				if len(parts) >= 3 {
					msgData = parts[2]
				}

				fmt.Printf("📨 [HENRY] Index=%s Cmd=%s Ver=%s Data=%s Checksum=%v\n",
					index, command, version, msgData, checksumOK)

				// Processar comandos conhecidos
				switch command {
				case "REON":
					// Evento de passagem de cartão — extrair matrícula dos dados
					handleREON(conn, index, msgData)

				case "RC":
					// Read Config — responder com valor da configuração
					fmt.Printf("📖 [HENRY] Leitura de config: %s\n", msgData)
					sendHenryResponse(conn, index, "RC", "00", msgData)

				case "EC":
					// Edit Config
					fmt.Printf("✏️  [HENRY] Escrita de config: %s\n", msgData)
					sendHenryResponse(conn, index, "EC", "00", "")

				case "EH":
					// Set DateTime
					fmt.Printf("🕐 [HENRY] Ajuste de relógio: %s\n", msgData)
					sendHenryResponse(conn, index, "EH", "00", "")

				default:
					// Comando desconhecido — responder com ACK genérico
					fmt.Printf("❓ [HENRY] Comando desconhecido: %s (hex: %x)\n", command, data)
					sendHenryResponse(conn, index, "AC", "00", "")
				}
			} else {
				// Mensagem muito curta — ACK genérico
				fmt.Printf("📨 [HENRY] Mensagem curta recebida: %x\n", data)
				response := buildHenryMessage("00", "AC+00+")
				conn.Write(response)
			}
		} else {
			fmt.Printf("⚠️  [HENRY] Dados sem STX/ETX: %x\n", data)
		}
	}
}

// handleREON processa evento de passagem de cartão/matrícula
func handleREON(conn net.Conn, index, data string) {
	// Dados do REON geralmente contém: matricula]mensagem]tipo
	parts := strings.Split(data, "]")

	if len(parts) >= 1 {
		cardID := parts[0]
		fmt.Printf("🪪  [HENRY] Cartão passado: %s\n", cardID)

		// Registrar automaticamente com toggle AUTO
		worker, appliedType, err := LogAttendance(cardID, "AUTO")
		if err != nil {
			msg := fmt.Sprintf("REON+00+30]40]ACESSO NEGADO]1")
			sendHenryResponse(conn, index, "", "", msg)
			fmt.Printf("🚫 [HENRY] Acesso negado para matrícula: %s\n", cardID)
		} else {
			var displayMsg string
			if appliedType == "ENTRADA" {
				displayMsg = fmt.Sprintf("BEM VINDO %s", worker.Name)
			} else {
				displayMsg = fmt.Sprintf("ATE LOGO %s", worker.Name)
			}
			msg := fmt.Sprintf("REON+00+6]40]%s]2", displayMsg)
			sendHenryResponse(conn, index, "", "", msg)
			fmt.Printf("✅ [HENRY] %s: %s (%s)\n", appliedType, worker.Name, cardID)
		}
	}
}

// henryChecksum calcula o checksum XOR do protocolo Henry
func henryChecksum(data []byte) byte {
	if len(data) == 0 {
		return 0
	}
	cs := data[0]
	for i := 1; i < len(data); i++ {
		cs ^= data[i]
	}
	return cs
}

// buildHenryMessage constrói uma mensagem no formato do protocolo Henry
func buildHenryMessage(index string, payload string) []byte {
	data := index + "+" + payload
	size := byte(len(data))

	// Montar: Size + 0x00 + data
	inner := append([]byte{size, 0x00}, []byte(data)...)
	cs := henryChecksum(inner)

	// STX + inner + checksum + ETX
	msg := []byte{STX}
	msg = append(msg, inner...)
	msg = append(msg, cs, ETX)
	return msg
}

// sendHenryResponse envia uma resposta formatada para a catraca
func sendHenryResponse(conn net.Conn, index, command, version, data string) {
	var payload string
	if command != "" {
		payload = command + "+" + version
		if data != "" {
			payload += "+" + data
		}
	} else {
		payload = data
	}

	msg := buildHenryMessage(index, payload)
	conn.Write(msg)
}
