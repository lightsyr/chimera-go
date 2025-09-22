def log_message(message):
    print(f"[LOG] {message}")

def handle_error(error):
    print(f"[ERROR] {error}")

def validate_input(data):
    if not isinstance(data, dict):
        handle_error("Invalid input: Expected a dictionary.")
        return False
    return True

def format_response(data):
    return {"status": "success", "data": data}