# Integração Catraca Henry 7x

Bem-vindo ao sistema de controle e integração para a **Catraca Henry 7x**.

Este sistema foi desenvolvido utilizando **Go (Golang)**. A principal razão para a escolha do Go foi a **facilidade de instalação e deploy**: o Go compila o sistema inteiro para um único arquivo executável, não exigindo a instalação de dependências complexas na máquina onde o sistema vai rodar. Basta baixar, executar e usar.

---

## Como Executar o Sistema

1. Certifique-se de ter o [Go instalado](https://go.dev/doc/install) na sua máquina.
2. Abra o terminal na pasta do projeto (`c:\catraca`).
3. Execute o comando:
   ```bash
   go run .
   ```
4. Acesse o painel pelo navegador em: **`http://localhost:8082`**

---

## Diário de Bordo: O código não funciona na minha catraca local

Pessoal, vou ser sincero, essa primeira versão tem **muitos erros**. Estou tentando arrumar a comunicação com a catraca física, mas tá bem difícil estabilizar a conexão TCP via protocolo Henry. 

Se você está rodando o código e não consegue conectar na sua catraca local, aqui estão os principais problemas que venho enfrentando e as soluções temporárias:

### 1. O Software Original está Aberto (Causa Muito Comum)
A catraca Henry 7x **só aceita 1 (uma) conexão ativa por vez** na sua porta de comunicação. 
Se você tiver o software original da Henry (como o Secullum ou Henry Config) rodando no seu computador (mesmo que em segundo plano), ele vai "sequestrar" a conexão. O nosso sistema em Go será bloqueado e não conseguirá se conectar. A comunicação cai na hora.
* **Solução:** Fechou um, abriu o outro. Tem que fechar completamente qualquer outro software de ponto antes de conectar pelo nosso painel.

### 2. IP ou Rede Incorretos
O seu computador e a catraca precisam estar na **mesma rede local**. A catraca não tem "wifi mágico".
* **Solução:** Tente dar um `ping` no IP da catraca pelo terminal (Ex: `ping 192.168.1.200`). Se não pingar, o problema é na rede, no cabo, ou a faixa de IP da catraca tá diferente do roteador. Você precisa configurar o IP certo direto no menu físico dela.

### 3. Modo de Operação da Catraca (Online vs Offline)
Para que o código consiga enviar comandos e ler passagens, a catraca precisa estar configurada no modo correto. Se ela estiver "offline", ela não escuta a porta de rede.
* **Solução:** Acesse o menu da própria catraca Henry e garanta que a Comunicação esteja configurada como **Online** (Modo Servidor). Só assim ela escuta a porta 3000.

### 4. Firewall do Windows 
O Windows adora bloquear portas não convencionais.
* **Solução:** Vá no Firewall do Windows e adicione uma exceção para o aplicativo em Go, ou libere a porta `3000`.

### 5. Você está conectando no 'Simulador' em vez da 'Catraca Física'
Eu criei um "Simulador de Catraca" porque tava sofrendo muito pra testar mudanças bobas sem ter o hardware do lado o tempo inteiro. O simulador é ativado pela váriavel `HENRY_SIMULATOR`.
* Se você quer usar a catraca **de verdade**, certifique-se de que não rodou com a flag do simulador ligada.
* Vá no painel web, aba "Catraca", e digite o **IP real** físico dela, não `localhost`.

---

## Como usar o Simulador

Caso você não tenha a catraca do lado e queira me ajudar a achar os bugs na interface visual:

1. No terminal Command Prompt/PowerShell, ative o simulador e rode o app:
   ```powershell
   $env:HENRY_SIMULATOR="1"
   go run .
   ```
2. No painel web (`http://localhost:8082`), vá na seção da catraca e digite `localhost` e porta `3000`. O sistema vai conversar com ele mesmo usando as regras do protocolo.

A jornada tá longa, mas a gente chega lá.
