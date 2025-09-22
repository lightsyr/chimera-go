import asyncio
import websockets
import json
from gamepad import Gamepad

async def handle_connection(websocket, path):
    gamepad = Gamepad()
    await websocket.send(json.dumps({"status": "Connected to gamepad server"}))

    try:
        while True:
            message = await websocket.recv()
            data = json.loads(message)
            action = data.get("action")

            if action:
                result = gamepad.execute_action(action)
                await websocket.send(json.dumps({"status": "Action executed", "result": result}))
            else:
                await websocket.send(json.dumps({"error": "No action specified"}))
    except websockets.exceptions.ConnectionClosed:
        print("Connection closed")

async def main():
    async with websockets.serve(handle_connection, "localhost", 8765):
        print("WebSocket server started on ws://localhost:8765")
        await asyncio.Future()  # run forever

if __name__ == "__main__":
    asyncio.run(main())