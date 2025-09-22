import asyncio
import websockets
import struct
from gamepad import Gamepad

async def handle_connection(websocket):
    gamepad = Gamepad()
    print("Cliente conectado!")

    try:
        async for message in websocket:
            if isinstance(message, bytes) and len(message) == 4:
                tipo, idx, valor = struct.unpack("<BBh", message)
                gamepad.handle_input(tipo, idx, valor)
    except websockets.exceptions.ConnectionClosed:
        print("Conexão fechada")

async def main():
    async with websockets.serve(handle_connection, "192.168.11.13", 8765):
        print("Servidor WebSocket rodando em ws://192.168.11.13:8765")
        await asyncio.Future()

if __name__ == "__main__":
    asyncio.run(main())
