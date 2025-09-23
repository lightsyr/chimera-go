import asyncio
import websockets
import struct
import socket
from gamepad import Gamepad

def get_local_ip():
    s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    try:
        # Conecta a um IP público qualquer, só pra descobrir IP local
        s.connect(("8.8.8.8", 80))
        ip = s.getsockname()[0]
    finally:
        s.close()
    return ip

LOCAL_IP = get_local_ip()

async def handle_connection(websocket):
    gamepad = Gamepad()
    print(f"Cliente conectado em {LOCAL_IP}!")

    try:
        async for message in websocket:
            if isinstance(message, bytes) and len(message) == 4:
                tipo, idx, valor = struct.unpack("<BBh", message)
                gamepad.handle_input(tipo, idx, valor)
    except websockets.exceptions.ConnectionClosed:
        print("Conexão fechada")

async def main():
    async with websockets.serve(handle_connection, LOCAL_IP, 8765):
        print(f"Servidor WebSocket rodando em ws://{LOCAL_IP}:8765")
        await asyncio.Future()

if __name__ == "__main__":
    asyncio.run(main())
