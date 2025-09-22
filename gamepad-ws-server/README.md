# Gamepad WebSocket Server

This project implements a WebSocket server that accepts gamepad inputs and executes corresponding actions on a Windows system.

## Project Structure

```
gamepad-ws-server
├── src
│   ├── server.py          # Entry point of the WebSocket server
│   ├── gamepad.py         # Handles gamepad input
│   └── utils.py           # Utility functions
├── requirements.txt       # Project dependencies
├── README.md              # Project documentation
└── .gitignore             # Git ignore file
```

## Setup Instructions

1. **Clone the repository:**
   ```
   git clone <repository-url>
   cd gamepad-ws-server
   ```

2. **Create a virtual environment (optional but recommended):**
   ```
   python -m venv venv
   ```

3. **Activate the virtual environment:**
   - On Windows:
     ```
     venv\Scripts\activate
     ```

4. **Install the required dependencies:**
   ```
   pip install -r requirements.txt
   ```

## Usage

1. **Run the WebSocket server:**
   ```
   python src/server.py
   ```

2. **Connect your gamepad and start sending inputs.**

## Dependencies

- `websockets`: For handling WebSocket connections.
- `pygame`: For reading gamepad inputs.

## License

This project is licensed under the MIT License. See the LICENSE file for details.