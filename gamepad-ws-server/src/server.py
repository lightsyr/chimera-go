import asyncio
import websockets
import logging
import signal
import sys
import time
import struct
from typing import Set, Dict, Any, Optional
from websockets.server import WebSocketServerProtocol
from gamepad import Gamepad

# Configure logging with more detail
logging.basicConfig(
    level=logging.DEBUG,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s',
    handlers=[
        logging.FileHandler('gamepad-server.log'),
        logging.StreamHandler(sys.stdout)
    ]
)
logger = logging.getLogger(__name__)

class GamepadServer:
    def __init__(self, listen_ip: str = "0.0.0.0", listen_port: int = 9000):
        self.listen_ip = listen_ip
        self.listen_port = listen_port
        self.gamepad: Optional[Gamepad] = None
        self.clients: Set[WebSocketServerProtocol] = set()
        self.running = False
        self.server = None
        self.stats = {
            'total_connections': 0,
            'active_connections': 0,
            'messages_received': 0,
            'messages_processed': 0,
            'errors': 0,
            'start_time': time.time()
        }

    async def initialize_gamepad(self) -> bool:
        """Initialize the gamepad controller with better error handling."""
        try:
            logger.info("Initializing gamepad controller...")
            self.gamepad = Gamepad()
            logger.info("Gamepad controller initialized successfully.")
            return True
        except ImportError as e:
            logger.error(f"Failed to import gamepad dependencies: {e}")
            logger.error("Make sure vgamepad is installed: pip install vgamepad")
            return False
        except Exception as e:
            logger.error(f"Failed to initialize gamepad: {e}")
            logger.error("Make sure you have the proper drivers installed")
            return False

    async def handle_client(self, websocket: WebSocketServerProtocol, path: str = "/"):
        """Handle individual WebSocket client connections with comprehensive error handling."""
        client_address = "unknown"
        
        try:
            # Get client address safely
            if hasattr(websocket, 'remote_address') and websocket.remote_address:
                client_address = f"{websocket.remote_address[0]}:{websocket.remote_address[1]}"
            
            logger.info(f"New client attempting to connect from {client_address}")
            
            # Add client to active connections
            self.clients.add(websocket)
            self.stats['total_connections'] += 1
            self.stats['active_connections'] += 1
            
            logger.info(f"Client {client_address} connected successfully. Active connections: {self.stats['active_connections']}")
            
            # Send welcome message
            try:
                await websocket.send("Welcome to Chimera Gamepad Server")
            except Exception as e:
                logger.warning(f"Could not send welcome message to {client_address}: {e}")
            
            # Main message loop
            async for message in websocket:
                try:
                    await self.process_message(websocket, message, client_address)
                except Exception as e:
                    logger.error(f"Error processing message from {client_address}: {e}")
                    self.stats['errors'] += 1
                    # Continue processing other messages instead of breaking
                    
        except websockets.exceptions.ConnectionClosed as e:
            logger.info(f"Client {client_address} disconnected normally (code: {e.code}, reason: {e.reason})")
        except websockets.exceptions.ConnectionClosedError as e:
            logger.warning(f"Client {client_address} connection closed with error: {e}")
        except websockets.exceptions.InvalidHandshake as e:
            logger.error(f"Invalid handshake from {client_address}: {e}")
        except websockets.exceptions.WebSocketException as e:
            logger.error(f"WebSocket error with {client_address}: {e}")
        except OSError as e:
            logger.error(f"OS error with {client_address}: {e}")
        except Exception as e:
            logger.error(f"Unexpected error handling client {client_address}: {e}")
            logger.exception("Full traceback:")
            self.stats['errors'] += 1
        finally:
            # Cleanup client connection
            try:
                if websocket in self.clients:
                    self.clients.remove(websocket)
                self.stats['active_connections'] -= 1
                logger.info(f"Client {client_address} cleanup completed. Active: {self.stats['active_connections']}")
            except Exception as e:
                logger.error(f"Error during client cleanup for {client_address}: {e}")

    async def process_message(self, websocket: WebSocketServerProtocol, message: Any, client_address: str):
        """Process incoming WebSocket message with detailed error handling."""
        self.stats['messages_received'] += 1
        
        try:
            if isinstance(message, bytes):
                await self.handle_binary_message(message, client_address)
            elif isinstance(message, str):
                await self.handle_text_message(message, client_address, websocket)
            else:
                logger.warning(f"Unknown message type from {client_address}: {type(message)}")
                
        except Exception as e:
            logger.error(f"Error processing message from {client_address}: {e}")
            logger.exception("Full traceback:")
            self.stats['errors'] += 1
            raise  # Re-raise to be handled by caller

    async def handle_binary_message(self, message: bytes, client_address: str):
        """Handle binary gamepad input messages with detailed validation."""
        
        # Validate message length
        if len(message) != 4:
            logger.warning(f"Invalid binary message length from {client_address}: {len(message)} bytes (expected 4)")
            return

        if not self.gamepad:
            logger.error(f"Gamepad not initialized, cannot process input from {client_address}")
            return

        try:
            # Unpack the binary message safely
            try:
                input_type, idx, value = struct.unpack('<BBh', message)
            except struct.error as e:
                logger.error(f"Error unpacking binary message from {client_address}: {e}")
                return
            
            # Validate input parameters
            if input_type not in [0, 1]:
                logger.warning(f"Invalid input type from {client_address}: {input_type}")
                return
            
            if input_type == 0 and (idx < 0 or idx > 5):  # Axis validation
                logger.warning(f"Invalid axis index from {client_address}: {idx}")
                return
                
            if input_type == 1 and (idx < 0 or idx > 13):  # Button validation
                logger.warning(f"Invalid button index from {client_address}: {idx}")
                return
            
            if not (-32768 <= value <= 32767):  # Value range validation
                logger.warning(f"Invalid value from {client_address}: {value}")
                return

            # Process the input
            self.gamepad.handle_input(input_type, idx, value)
            self.stats['messages_processed'] += 1
            
            # Debug logging for first few messages
            if self.stats['messages_processed'] <= 10:
                logger.debug(f"Processed input from {client_address}: type={input_type}, idx={idx}, value={value}")
            
        except Exception as e:
            logger.error(f"Error processing binary message from {client_address}: {e}")
            logger.exception("Full traceback:")
            self.stats['errors'] += 1
            raise

    async def handle_text_message(self, message: str, client_address: str, websocket: WebSocketServerProtocol):
        """Handle text messages with proper response handling."""
        try:
            message = message.strip().lower()
            
            if message == "ping":
                try:
                    await websocket.send("pong")
                except Exception as e:
                    logger.error(f"Error sending pong to {client_address}: {e}")
                    
            elif message == "status":
                await self.send_status_to_client(websocket, client_address)
                
            elif message == "reset":
                if self.gamepad:
                    try:
                        self.gamepad.reset()
                        logger.info(f"Gamepad reset requested by {client_address}")
                        await websocket.send("Gamepad reset successfully")
                    except Exception as e:
                        logger.error(f"Error resetting gamepad for {client_address}: {e}")
                        await websocket.send(f"Error resetting gamepad: {e}")
                else:
                    await websocket.send("Gamepad not initialized")
                    
            else:
                logger.info(f"Unknown text message from {client_address}: {message}")
                await websocket.send(f"Unknown command: {message}")
                
        except Exception as e:
            logger.error(f"Error handling text message from {client_address}: {e}")
            logger.exception("Full traceback:")

    async def send_status_to_client(self, websocket: WebSocketServerProtocol, client_address: str):
        """Send status information back to client."""
        try:
            uptime = time.time() - self.stats['start_time']
            
            status = {
                'server_stats': self.stats.copy(),
                'gamepad_status': self.gamepad.get_status() if self.gamepad else None,
                'uptime_seconds': uptime,
                'uptime_formatted': f"{uptime:.1f}s"
            }
            
            status_json = str(status)  # Simple string conversion
            await websocket.send(status_json)
            logger.debug(f"Status sent to {client_address}")
                    
        except Exception as e:
            logger.error(f"Error sending status to {client_address}: {e}")
            logger.exception("Full traceback:")

    async def start_server(self):
        """Start the WebSocket server with comprehensive error handling."""
        
        # Initialize gamepad first
        if not await self.initialize_gamepad():
            logger.error("Cannot start server: gamepad initialization failed")
            return False

        try:
            logger.info(f"Starting WebSocket server on {self.listen_ip}:{self.listen_port}")
            
            # Create server with reasonable settings
            self.server = await websockets.serve(
                self.handle_client,
                self.listen_ip,
                self.listen_port,
                ping_interval=30,    # Send ping every 30 seconds
                ping_timeout=10,     # Wait 10 seconds for pong
                close_timeout=10,    # Wait 10 seconds for close
                max_size=1024,       # 1KB max message size (plenty for gamepad data)
                max_queue=32,        # Max 32 messages in queue
                compression=None,    # Disable compression for lower latency
            )
            
            self.running = True
            logger.info(f"WebSocket server started successfully on ws://{self.listen_ip}:{self.listen_port}")
            
            # Start periodic status logging
            asyncio.create_task(self.log_status_periodically())
            
            return True
            
        except OSError as e:
            logger.error(f"OS error starting server: {e}")
            if "Address already in use" in str(e):
                logger.error(f"Port {self.listen_port} is already in use. Please check if another instance is running.")
            return False
        except Exception as e:
            logger.error(f"Failed to start server: {e}")
            logger.exception("Full traceback:")
            return False

    async def log_status_periodically(self):
        """Log server status periodically."""
        while self.running:
            try:
                await asyncio.sleep(60)  # Log every minute
                uptime = time.time() - self.stats['start_time']
                
                # Calculate rates
                message_rate = self.stats['messages_processed'] / max(uptime, 1)
                error_rate = self.stats['errors'] / max(uptime, 1)
                
                logger.info(
                    f"Server Status - Uptime: {uptime:.1f}s, "
                    f"Active: {self.stats['active_connections']}, "
                    f"Total: {self.stats['total_connections']}, "
                    f"Messages: {self.stats['messages_processed']}/{self.stats['messages_received']}, "
                    f"Errors: {self.stats['errors']}, "
                    f"Msg Rate: {message_rate:.2f}/s, "
                    f"Error Rate: {error_rate:.4f}/s"
                )
            except Exception as e:
                logger.error(f"Error in status logging: {e}")

    async def shutdown(self):
        """Shutdown the server gracefully."""
        logger.info("Starting server shutdown...")
        self.running = False
        
        # Reset gamepad to default state
        if self.gamepad:
            try:
                self.gamepad.reset()
                logger.info("Gamepad reset to default state")
            except Exception as e:
                logger.error(f"Error resetting gamepad during shutdown: {e}")
        
        # Close all client connections
        if self.clients:
            logger.info(f"Closing {len(self.clients)} active connections...")
            close_tasks = []
            for client in self.clients.copy():
                try:
                    close_tasks.append(client.close())
                except Exception as e:
                    logger.warning(f"Error creating close task for client: {e}")
            
            if close_tasks:
                await asyncio.gather(*close_tasks, return_exceptions=True)
        
        # Close the server
        if self.server:
            try:
                self.server.close()
                await self.server.wait_closed()
                logger.info("Server closed successfully")
            except Exception as e:
                logger.error(f"Error closing server: {e}")
        
        # Log final statistics
        uptime = time.time() - self.stats['start_time']
        logger.info(
            f"Final Stats - Uptime: {uptime:.1f}s, "
            f"Total Connections: {self.stats['total_connections']}, "
            f"Messages Processed: {self.stats['messages_processed']}, "
            f"Errors: {self.stats['errors']}"
        )

async def main():
    """Main entry point with signal handling."""
    server = GamepadServer()
    
    # Setup signal handlers
    def signal_handler(signum, frame):
        logger.info(f"Received signal {signum}")
        asyncio.create_task(server.shutdown())
    
    signal.signal(signal.SIGINT, signal_handler)
    signal.signal(signal.SIGTERM, signal_handler)
    
    try:
        if await server.start_server():
            # Keep the server running
            await server.server.wait_closed()
        else:
            logger.error("Failed to start server")
            return 1
            
    except KeyboardInterrupt:
        logger.info("Keyboard interrupt received")
    except Exception as e:
        logger.error(f"Unexpected error in main: {e}")
        logger.exception("Full traceback:")
        return 1
    finally:
        await server.shutdown()
    
    return 0

if __name__ == "__main__":
    try:
        exit_code = asyncio.run(main())
        sys.exit(exit_code)
    except KeyboardInterrupt:
        print("\n[Python] Server shutdown by user.")
        sys.exit(0)
    except Exception as e:
        print(f"[Python] Fatal error: {e}")
        sys.exit(1)