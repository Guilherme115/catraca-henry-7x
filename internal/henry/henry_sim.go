package henry

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/user/catraca/internal/db"
)

const (
	STX = 0x02
	ETX = 0x03
)

func StartHenrySimulator() {
	ln, err := net.Listen("tcp", ":3000")
	if err != nil {
		fmt.Println("Erro ao iniciar simulador Henry:", err)
		return
	}
	defer ln.Close()
	fmt.Println("Simulador Henry ouvindo na porta 3000...")

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

	fmt.Printf("\n[HENRY] Conexao de: %s\n", remoteAddr)

	for {
		n, err := conn.Read(buf)
		if err != nil {
			fmt.Printf("[HENRY] Desconectado: %s\n", remoteAddr)
			return
		}

		data := buf[:n]
		if len(data) < 2 {
			continue
		}

		if data[0] == STX && data[len(data)-1] == ETX {
			payload := data[1 : len(data)-2]

			receivedChecksum := data[len(data)-2]
			calculatedChecksum := henryChecksum(data[1 : len(data)-2])

			checksumOK := receivedChecksum == calculatedChecksum

			if len(payload) >= 4 {
				index := string(payload[2:4])

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

				fmt.Printf("[HENRY] Index=%s Cmd=%s Ver=%s Data=%s Checksum=%v\n",
					index, command, version, msgData, checksumOK)

				switch command {
				case "REON":
					handleREON(conn, index, msgData)

				case "RC":
					fmt.Printf("[HENRY] Leitura de config: %s\n", msgData)
					sendHenryResponse(conn, index, "RC", "00", msgData)

				case "EC":
					fmt.Printf("[HENRY] Escrita de config: %s\n", msgData)
					sendHenryResponse(conn, index, "EC", "00", "")

				case "EH":
					fmt.Printf("[HENRY] Ajuste de relogio: %s\n", msgData)
					sendHenryResponse(conn, index, "EH", "00", "")

				default:
					fmt.Printf("[HENRY] Comando desconhecido: %s (hex: %x)\n", command, data)
					sendHenryResponse(conn, index, "AC", "00", "")
				}
			} else {
				fmt.Printf("[HENRY] Mensagem curta recebida: %x\n", data)
				response := buildHenryMessage("00", "AC+00+")
				conn.Write(response)
			}
		} else {
			fmt.Printf("[HENRY] Dados sem STX/ETX: %x\n", data)
		}
	}
}

func handleREON(conn net.Conn, index, data string) {
	parts := strings.Split(data, "]")

	if len(parts) >= 1 {
		cardID := parts[0]
		fmt.Printf("[HENRY] Cartao passado: %s\n", cardID)

		worker, appliedType, err := db.LogAttendance(cardID, "AUTO")
		if err != nil {
			msg := fmt.Sprintf("REON+00+30]40]ACESSO NEGADO]1")
			sendHenryResponse(conn, index, "", "", msg)
			fmt.Printf("[HENRY] Acesso negado para matricula: %s\n", cardID)
		} else {
			var displayMsg string
			if appliedType == "ENTRADA" {
				displayMsg = fmt.Sprintf("BEM VINDO %s", worker.Name)
			} else {
				displayMsg = fmt.Sprintf("ATE LOGO %s", worker.Name)
			}
			msg := fmt.Sprintf("REON+00+6]40]%s]2", displayMsg)
			sendHenryResponse(conn, index, "", "", msg)
			fmt.Printf("[HENRY] %s: %s (%s)\n", appliedType, worker.Name, cardID)
		}
	}
}

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

func buildHenryMessage(index string, payload string) []byte {
	data := index + "+" + payload
	size := byte(len(data))

	inner := append([]byte{size, 0x00}, []byte(data)...)
	cs := henryChecksum(inner)

	msg := []byte{STX}
	msg = append(msg, inner...)
	msg = append(msg, cs, ETX)
	return msg
}

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
