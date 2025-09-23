import asyncio
import websockets
from gamepad import Gamepad

gp = Gamepad()

LISTEN_IP = "0.0.0.0"
LISTEN_PORT = 9000

print(f"[Python] Servidor WebSocket pronto em ws://{LISTEN_IP}:{LISTEN_PORT}")

async def handler(websocket):
    print(f"[Python] Cliente WebSocket conectado de {websocket.remote_address}")
    async for message in websocket:
        if isinstance(message, bytes) and len(message) == 4:
            tipo = message[0]
            idx = message[1]
            valor = int.from_bytes(message[2:4], byteorder='little', signed=True)
            gp.handle_input(tipo, idx, valor)
        else:
            print(f"[Python] Pacote inválido recebido: {message}")
            
    print(f"[Python] Cliente {websocket.remote_address} desconectado.")

async def main():
    async with websockets.serve(handler, LISTEN_IP, LISTEN_PORT):
        await asyncio.Future()  # Mantém o servidor rodando

if __name__ == "__main__":
    try:
        asyncio.run(main())
    except KeyboardInterrupt:
        print("[Python] Servidor encerrado.")