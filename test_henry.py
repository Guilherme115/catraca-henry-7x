
import socket

def henry_checksum(data):
    if not data:
        return 0
    cs = data[0]
    for b in data[1:]:
        cs ^= b
    return cs

def test_henry_reon(card_id):
    # Formato: REON + 00 + matrícula ] mensagem ] tipo
    payload = f"REON+00+{card_id}]TESTE]1"
    index = "01"
    data_to_send = f"{index}+{payload}".encode('ascii')
    
    # Inner: Size + 0x00 + data
    size = len(data_to_send)
    inner = bytes([size, 0x00]) + data_to_send
    
    cs = henry_checksum(inner)
    
    # Full: STX + inner + CS + ETX
    msg = bytes([0x02]) + inner + bytes([cs, 0x03])
    
    print(f"Enviando para porta 3000: {msg.hex()}")
    
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.connect(('localhost', 3000))
        s.sendall(msg)
        response = s.recv(1024)
        print(f"Resposta da Catraca: {response.hex()}")
        # Tenta decodificar a resposta (STX...ETX)
        if len(response) > 5:
            content = response[3:-2].decode('ascii', errors='ignore')
            print(f"Conteúdo da Resposta: {content}")

if __name__ == "__main__":
    test_henry_reon("9999")
