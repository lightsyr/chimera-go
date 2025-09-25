import logging
import time
import json
import sys
from typing import Any, Dict, Optional, Union
from datetime import datetime
import traceback

# Configure logger
logger = logging.getLogger(__name__)

class ChimeraUtils:
    """Utility class for Chimera game streaming server."""
    
    @staticmethod
    def setup_logging(log_file: str = "chimera.log", level: int = logging.INFO) -> logging.Logger:
        """Setup comprehensive logging configuration."""
        
        # Create formatter
        formatter = logging.Formatter(
            '%(asctime)s - %(name)s - %(levelname)s - [%(filename)s:%(lineno)d] - %(message)s'
        )
        
        # Create file handler
        file_handler = logging.FileHandler(log_file, encoding='utf-8')
        file_handler.setLevel(level)
        file_handler.setFormatter(formatter)
        
        # Create console handler
        console_handler = logging.StreamHandler(sys.stdout)
        console_handler.setLevel(level)
        console_handler.setFormatter(formatter)
        
        # Configure root logger
        root_logger = logging.getLogger()
        root_logger.setLevel(level)
        root_logger.handlers = []  # Clear existing handlers
        root_logger.addHandler(file_handler)
        root_logger.addHandler(console_handler)
        
        return root_logger

    @staticmethod
    def log_message(message: str, level: str = "INFO", extra_data: Optional[Dict] = None) -> None:
        """Enhanced logging with structured data."""
        
        log_entry = {
            "timestamp": datetime.now().isoformat(),
            "message": message,
            "level": level.upper()
        }
        
        if extra_data:
            log_entry.update(extra_data)
        
        # Log based on level
        if level.upper() == "DEBUG":
            logger.debug(message, extra=extra_data or {})
        elif level.upper() == "INFO":
            logger.info(message, extra=extra_data or {})
        elif level.upper() == "WARNING":
            logger.warning(message, extra=extra_data or {})
        elif level.upper() == "ERROR":
            logger.error(message, extra=extra_data or {})
        elif level.upper() == "CRITICAL":
            logger.critical(message, extra=extra_data or {})

    @staticmethod
    def handle_error(error: Exception, context: str = "", reraise: bool = False) -> None:
        """Enhanced error handling with context and stack traces."""
        
        error_info = {
            "error_type": type(error).__name__,
            "error_message": str(error),
            "context": context,
            "traceback": traceback.format_exc(),
            "timestamp": datetime.now().isoformat()
        }
        
        logger.error(f"Error in {context}: {error}", extra=error_info)
        
        if reraise:
            raise error

    @staticmethod
    def validate_input(data: Any, expected_type: type = dict, required_fields: Optional[list] = None) -> bool:
        """Enhanced input validation with type checking and required fields."""
        
        if not isinstance(data, expected_type):
            ChimeraUtils.log_message(
                f"Invalid input type. Expected {expected_type.__name__}, got {type(data).__name__}",
                "ERROR"
            )
            return False
        
        if required_fields and isinstance(data, dict):
            missing_fields = [field for field in required_fields if field not in data]
            if missing_fields:
                ChimeraUtils.log_message(
                    f"Missing required fields: {missing_fields}",
                    "ERROR"
                )
                return False
        
        return True

    @staticmethod
    def format_response(
        data: Any = None, 
        success: bool = True, 
        message: str = "", 
        error_code: Optional[str] = None
    ) -> Dict[str, Any]:
        """Standardized response formatting."""
        
        response = {
            "success": success,
            "timestamp": datetime.now().isoformat(),
            "data": data
        }
        
        if message:
            response["message"] = message
        
        if error_code:
            response["error_code"] = error_code
        
        return response

    @staticmethod
    def validate_gamepad_input(input_type: int, idx: int, value: int) -> bool:
        """Validate gamepad input parameters."""
        
        # Validate input type
        if input_type not in [0, 1]:
            ChimeraUtils.log_message(
                f"Invalid input type: {input_type}. Must be 0 (axis) or 1 (button)",
                "ERROR"
            )
            return False
        
        # Validate axis indices
        if input_type == 0 and (idx < 0 or idx > 5):
            ChimeraUtils.log_message(
                f"Invalid axis index: {idx}. Must be between 0-5",
                "ERROR"
            )
            return False
        
        # Validate button indices
        if input_type == 1 and (idx < 0 or idx > 13):
            ChimeraUtils.log_message(
                f"Invalid button index: {idx}. Must be between 0-13",
                "ERROR"
            )
            return False
        
        # Validate value range
        if not (-32768 <= value <= 32767):
            ChimeraUtils.log_message(
                f"Invalid value: {value}. Must be between -32768 and 32767",
                "ERROR"
            )
            return False
        
        return True

    @staticmethod
    def validate_stream_config(config: Dict[str, Any]) -> bool:
        """Validate streaming configuration parameters."""
        
        required_fields = ["width", "height", "fps", "codec"]
        if not ChimeraUtils.validate_input(config, dict, required_fields):
            return False
        
        # Validate resolution
        width = config.get("width", 0)
        height = config.get("height", 0)
        
        if not (320 <= width <= 3840 and 240 <= height <= 2160):
            ChimeraUtils.log_message(
                f"Invalid resolution: {width}x{height}. Must be between 320x240 and 3840x2160",
                "ERROR"
            )
            return False
        
        # Validate FPS
        fps = config.get("fps", 0)
        if not (1 <= fps <= 144):
            ChimeraUtils.log_message(
                f"Invalid FPS: {fps}. Must be between 1 and 144",
                "ERROR"
            )
            return False
        
        # Validate codec
        supported_codecs = ["h264", "h265", "vp8", "vp9"]
        codec = config.get("codec", "").lower()
        if codec not in supported_codecs:
            ChimeraUtils.log_message(
                f"Unsupported codec: {codec}. Supported: {supported_codecs}",
                "ERROR"
            )
            return False
        
        return True

    @staticmethod
    def calculate_bitrate(width: int, height: int, fps: int, quality: str = "medium") -> int:
        """Calculate appropriate bitrate based on resolution and quality."""
        
        pixel_count = width * height
        base_bitrate = pixel_count * fps
        
        # Quality multipliers
        quality_multipliers = {
            "low": 0.1,
            "medium": 0.2,
            "high": 0.3,
            "ultra": 0.5
        }
        
        multiplier = quality_multipliers.get(quality.lower(), 0.2)
        calculated_bitrate = int(base_bitrate * multiplier / 1000)  # Convert to Kbps
        
        # Clamp to reasonable ranges
        min_bitrate = 500  # 500 Kbps minimum
        max_bitrate = 50000  # 50 Mbps maximum
        
        return max(min_bitrate, min(calculated_bitrate, max_bitrate))

    @staticmethod
    def format_bytes(bytes_count: int) -> str:
        """Format byte count in human readable format."""
        
        for unit in ['B', 'KB', 'MB', 'GB', 'TB']:
            if bytes_count < 1024.0:
                return f"{bytes_count:.2f} {unit}"
            bytes_count /= 1024.0
        return f"{bytes_count:.2f} PB"

    @staticmethod
    def format_duration(seconds: float) -> str:
        """Format duration in human readable format."""
        
        if seconds < 60:
            return f"{seconds:.1f}s"
        elif seconds < 3600:
            minutes = int(seconds // 60)
            secs = int(seconds % 60)
            return f"{minutes}m {secs}s"
        else:
            hours = int(seconds // 3600)
            minutes = int((seconds % 3600) // 60)
            return f"{hours}h {minutes}m"

    @staticmethod
    def create_performance_report(metrics: Dict[str, Any]) -> Dict[str, Any]:
        """Generate comprehensive performance report."""
        
        report = {
            "timestamp": datetime.now().isoformat(),
            "summary": {},
            "metrics": metrics.copy(),
            "recommendations": []
        }
        
        # Calculate summary statistics
        if "frames_processed" in metrics and "frames_dropped" in metrics:
            total_frames = metrics["frames_processed"]
            dropped_frames = metrics["frames_dropped"]
            
            if total_frames > 0:
                drop_rate = (dropped_frames / total_frames) * 100
                report["summary"]["drop_rate_percent"] = round(drop_rate, 2)
                
                if drop_rate > 10:
                    report["recommendations"].append("High frame drop rate detected. Consider reducing quality or resolution.")
                elif drop_rate > 5:
                    report["recommendations"].append("Moderate frame drops detected. Monitor system resources.")
        
        # Memory usage analysis
        if "memory_usage" in metrics:
            memory_mb = metrics["memory_usage"] / (1024 * 1024)
            report["summary"]["memory_usage_mb"] = round(memory_mb, 2)
            
            if memory_mb > 1000:
                report["recommendations"].append("High memory usage detected. Consider optimizing buffer sizes.")
        
        # Connection quality analysis
        if "active_streams" in metrics:
            active_streams = metrics["active_streams"]
            report["summary"]["active_streams"] = active_streams
            
            if active_streams > 5:
                report["recommendations"].append("High number of concurrent streams. Monitor server performance.")
        
        return report

    @staticmethod
    def sanitize_filename(filename: str) -> str:
        """Sanitize filename for safe file operations."""
        
        import re
        # Remove dangerous characters
        filename = re.sub(r'[<>:"/\\|?*]', '_', filename)
        # Remove control characters
        filename = ''.join(char for char in filename if ord(char) >= 32)
        # Limit length
        if len(filename) > 255:
            name, ext = filename.rsplit('.', 1) if '.' in filename else (filename, '')
            filename = name[:250] + ('.' + ext if ext else '')
        
        return filename

    @staticmethod
    def get_system_info() -> Dict[str, Any]:
        """Get basic system information for debugging."""
        
        import platform
        import psutil
        
        try:
            info = {
                "platform": platform.system(),
                "platform_version": platform.version(),
                "python_version": platform.python_version(),
                "cpu_count": psutil.cpu_count(),
                "memory_total": psutil.virtual_memory().total,
                "memory_available": psutil.virtual_memory().available,
                "disk_usage": {
                    "total": psutil.disk_usage('/').total if platform.system() != 'Windows' else psutil.disk_usage('C:').total,
                    "free": psutil.disk_usage('/').free if platform.system() != 'Windows' else psutil.disk_usage('C:').free
                }
            }
            return info
        except Exception as e:
            ChimeraUtils.handle_error(e, "get_system_info")
            return {"error": "Unable to retrieve system information"}

# Singleton instance for global utilities
utils = ChimeraUtils()

# Convenience functions for backward compatibility
def log_message(message: str, level: str = "INFO", extra_data: Optional[Dict] = None) -> None:
    """Convenience function for logging."""
    utils.log_message(message, level, extra_data)

def handle_error(error: Exception, context: str = "", reraise: bool = False) -> None:
    """Convenience function for error handling."""
    utils.handle_error(error, context, reraise)

def validate_input(data: Any, expected_type: type = dict, required_fields: Optional[list] = None) -> bool:
    """Convenience function for input validation."""
    return utils.validate_input(data, expected_type, required_fields)

def format_response(data: Any = None, success: bool = True, message: str = "", error_code: Optional[str] = None) -> Dict[str, Any]:
    """Convenience function for response formatting."""
    return utils.format_response(data, success, message, error_code)

# Performance monitoring decorator
def monitor_performance(func_name: str = ""):
    """Decorator to monitor function performance."""
    def decorator(func):
        def wrapper(*args, **kwargs):
            start_time = time.time()
            try:
                result = func(*args, **kwargs)
                execution_time = time.time() - start_time
                log_message(
                    f"Function {func_name or func.__name__} executed successfully",
                    "DEBUG",
                    {"execution_time": execution_time}
                )
                return result
            except Exception as e:
                execution_time = time.time() - start_time
                handle_error(
                    e, 
                    f"Function {func_name or func.__name__} (execution_time: {execution_time:.3f}s)"
                )
                raise
        return wrapper
    return decorator