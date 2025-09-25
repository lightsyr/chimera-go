#!/usr/bin/env python3
"""
Diagnostic script for Chimera Game Streaming Server
This script helps identify common issues and provides solutions.
"""

import sys
import os
import logging
import traceback
import subprocess
import importlib
import platform
from pathlib import Path

# Setup basic logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)

class ChimeraDiagnostic:
    def __init__(self):
        self.issues_found = []
        self.solutions = []
        
    def log_issue(self, issue, solution=""):
        self.issues_found.append(issue)
        if solution:
            self.solutions.append(solution)
        logger.error(f"ISSUE: {issue}")
        if solution:
            logger.info(f"SOLUTION: {solution}")

    def check_python_version(self):
        """Check if Python version is compatible."""
        logger.info("Checking Python version...")
        
        version = sys.version_info
        if version.major < 3 or (version.major == 3 and version.minor < 8):
            self.log_issue(
                f"Python version {version.major}.{version.minor} is too old",
                "Install Python 3.8 or newer"
            )
            return False
        else:
            logger.info(f"Python version {version.major}.{version.minor}.{version.micro} - OK")
            return True

    def check_required_modules(self):
        """Check if all required Python modules are available."""
        logger.info("Checking required Python modules...")
        
        required_modules = [
            ('asyncio', 'Built-in Python module'),
            ('websockets', 'pip install websockets'),
            ('vgamepad', 'pip install vgamepad'),
            ('logging', 'Built-in Python module'),
            ('struct', 'Built-in Python module'),
            ('time', 'Built-in Python module'),
        ]
        
        all_ok = True
        for module_name, install_cmd in required_modules:
            try:
                importlib.import_module(module_name)
                logger.info(f"Module '{module_name}' - OK")
            except ImportError as e:
                self.log_issue(
                    f"Module '{module_name}' not found",
                    f"Install with: {install_cmd}"
                )
                all_ok = False
        
        return all_ok

    def check_vgamepad_drivers(self):
        """Check if ViGEm drivers are installed (Windows only)."""
        if platform.system() != "Windows":
            logger.info("Skipping ViGEm driver check (not Windows)")
            return True
            
        logger.info("Checking ViGEm drivers...")
        
        try:
            import vgamepad as vg
            # Try to create a gamepad to test drivers
            test_gamepad = vg.VX360Gamepad()
            logger.info("ViGEm drivers - OK")
            return True
        except Exception as e:
            self.log_issue(
                f"ViGEm drivers not working: {e}",
                "Install ViGEm Bus Driver from: https://github.com/ViGEm/ViGEmBus/releases"
            )
            return False

    def check_network_ports(self):
        """Check if required network ports are available."""
        logger.info("Checking network ports...")
        
        import socket
        ports_to_check = [8080, 9000]  # HTTP and WebSocket ports
        
        all_ok = True
        for port in ports_to_check:
            try:
                with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
                    s.bind(('localhost', port))
                logger.info(f"Port {port} - Available")
            except OSError as e:
                self.log_issue(
                    f"Port {port} is not available: {e}",
                    f"Stop any service using port {port} or change the port in configuration"
                )
                all_ok = False
        
        return all_ok

    def check_file_structure(self):
        """Check if all required files are present."""
        logger.info("Checking file structure...")
        
        required_files = [
            'main.go',
            'gamepad.py',
            'server.py', 
            'index.html',
            'utils.py'
        ]
        
        all_ok = True
        current_dir = Path('.')
        
        for filename in required_files:
            file_path = current_dir / filename
            if file_path.exists():
                logger.info(f"File '{filename}' - OK")
            else:
                # Also check in common subdirectories
                found = False
                for subdir in ['gamepad-ws-server/src', 'web', '.']:
                    alt_path = current_dir / subdir / filename
                    if alt_path.exists():
                        logger.info(f"File '{filename}' found in '{subdir}' - OK")
                        found = True
                        break
                
                if not found:
                    self.log_issue(
                        f"Required file '{filename}' not found",
                        f"Make sure '{filename}' is in the correct directory"
                    )
                    all_ok = False
        
        return all_ok

    def test_websocket_server(self):
        """Test if WebSocket server can start."""
        logger.info("Testing WebSocket server startup...")
        
        try:
            # Import our modules
            sys.path.append('.')
            sys.path.append('gamepad-ws-server/src')
            
            from gamepad import Gamepad
            
            # Test gamepad creation
            gamepad = Gamepad()
            if gamepad.initialized:
                logger.info("Gamepad initialization - OK")
                gamepad.reset()
                return True
            else:
                self.log_issue(
                    "Gamepad failed to initialize",
                    "Check ViGEm drivers and vgamepad installation"
                )
                return False
                
        except ImportError as e:
            self.log_issue(
                f"Cannot import required modules: {e}",
                "Check module installation and file paths"
            )
            return False
        except Exception as e:
            self.log_issue(
                f"Error testing gamepad: {e}",
                "Check error details and driver installation"
            )
            return False

    def check_go_environment(self):
        """Check if Go environment is set up correctly."""
        logger.info("Checking Go environment...")
        
        try:
            # Check if Go is installed
            result = subprocess.run(['go', 'version'], 
                                  capture_output=True, 
                                  text=True, 
                                  timeout=10)
            
            if result.returncode == 0:
                logger.info(f"Go version: {result.stdout.strip()} - OK")
                
                # Check if required Go modules are available
                go_mod_check = subprocess.run(['go', 'mod', 'tidy'], 
                                            capture_output=True, 
                                            text=True, 
                                            timeout=30)
                
                if go_mod_check.returncode == 0:
                    logger.info("Go modules - OK")
                    return True
                else:
                    self.log_issue(
                        f"Go module issues: {go_mod_check.stderr}",
                        "Run 'go mod tidy' to fix dependencies"
                    )
                    return False
            else:
                self.log_issue(
                    "Go command failed",
                    "Make sure Go is properly installed and in PATH"
                )
                return False
                
        except FileNotFoundError:
            self.log_issue(
                "Go not found",
                "Install Go from https://golang.org/dl/"
            )
            return False
        except subprocess.TimeoutExpired:
            self.log_issue(
                "Go command timeout",
                "Check Go installation and network connection"
            )
            return False

    def check_ffmpeg(self):
        """Check if FFmpeg is available."""
        logger.info("Checking FFmpeg...")
        
        try:
            result = subprocess.run(['ffmpeg', '-version'], 
                                  capture_output=True, 
                                  text=True, 
                                  timeout=10)
            
            if result.returncode == 0:
                # Extract version info
                version_line = result.stdout.split('\n')[0]
                logger.info(f"FFmpeg: {version_line} - OK")
                return True
            else:
                self.log_issue(
                    "FFmpeg command failed",
                    "Reinstall FFmpeg"
                )
                return False
                
        except FileNotFoundError:
            self.log_issue(
                "FFmpeg not found",
                "Install FFmpeg from https://ffmpeg.org/download.html"
            )
            return False
        except subprocess.TimeoutExpired:
            self.log_issue(
                "FFmpeg command timeout",
                "Check FFmpeg installation"
            )
            return False

    def run_full_diagnostic(self):
        """Run complete diagnostic check."""
        logger.info("="*50)
        logger.info("Starting Chimera Diagnostic Check")
        logger.info("="*50)
        
        checks = [
            ("Python Version", self.check_python_version),
            ("Required Modules", self.check_required_modules),
            ("ViGEm Drivers", self.check_vgamepad_drivers),
            ("Network Ports", self.check_network_ports),
            ("File Structure", self.check_file_structure),
            ("WebSocket Server", self.test_websocket_server),
            ("Go Environment", self.check_go_environment),
            ("FFmpeg", self.check_ffmpeg),
        ]
        
        results = {}
        for check_name, check_func in checks:
            logger.info(f"\n--- {check_name} ---")
            try:
                results[check_name] = check_func()
            except Exception as e:
                logger.error(f"Error during {check_name}: {e}")
                logger.error(traceback.format_exc())
                results[check_name] = False
        
        # Summary
        logger.info("\n" + "="*50)
        logger.info("DIAGNOSTIC SUMMARY")
        logger.info("="*50)
        
        passed = sum(1 for result in results.values() if result)
        total = len(results)
        
        logger.info(f"Checks passed: {passed}/{total}")
        
        if self.issues_found:
            logger.info(f"\nISSUES FOUND ({len(self.issues_found)}):")
            for i, issue in enumerate(self.issues_found, 1):
                logger.info(f"{i}. {issue}")
            
            logger.info(f"\nRECOMMENDED SOLUTIONS:")
            for i, solution in enumerate(self.solutions, 1):
                logger.info(f"{i}. {solution}")
        else:
            logger.info("No issues found! System appears to be ready.")
        
        return len(self.issues_found) == 0

def main():
    """Main diagnostic function."""
    print("Chimera Game Streaming Server - Diagnostic Tool")
    print("=" * 60)
    
    diagnostic = ChimeraDiagnostic()
    success = diagnostic.run_full_diagnostic()
    
    if success:
        print("\n✅ All checks passed! You can start the server.")
        return 0
    else:
        print(f"\n❌ Found {len(diagnostic.issues_found)} issues that need to be resolved.")
        return 1

if __name__ == "__main__":
    try:
        exit_code = main()
        sys.exit(exit_code)
    except KeyboardInterrupt:
        print("\nDiagnostic interrupted by user.")
        sys.exit(1)
    except Exception as e:
        print(f"Diagnostic failed with error: {e}")
        traceback.print_exc()
        sys.exit(1)