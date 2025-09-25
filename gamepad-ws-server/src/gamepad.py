import logging
import time
from typing import Dict, Optional
import sys

# Setup logger
logger = logging.getLogger(__name__)

try:
    import vgamepad as vg
    VGAMEPAD_AVAILABLE = True
    logger.info("vgamepad imported successfully")
except ImportError as e:
    VGAMEPAD_AVAILABLE = False
    logger.error(f"vgamepad not available: {e}")
    logger.error("Install with: pip install vgamepad")

class Gamepad:
    def __init__(self):
        """Initialize the Xbox 360 virtual gamepad with comprehensive error handling."""
        self.vgpad = None
        self.axes = {
            'lx': 0.0, 'ly': 0.0,  # Left Stick
            'rx': 0.0, 'ry': 0.0,  # Right Stick
            'lt': 0.0, 'rt': 0.0   # Left/Right Trigger
        }
        self.buttons_state = {}  # Track button states
        self.last_update = 0
        self.update_threshold = 1.0 / 120.0  # 120 Hz max update rate
        self.initialized = False
        
        if not VGAMEPAD_AVAILABLE:
            logger.error("[Gamepad] Cannot initialize: vgamepad not available")
            raise ImportError("vgamepad library not available. Install with: pip install vgamepad")
        
        try:
            logger.info("[Gamepad] Attempting to create Xbox 360 virtual controller...")
            self.vgpad = vg.VX360Gamepad()
            self.initialized = True
            logger.info("[Gamepad] Xbox 360 virtual controller initialized successfully")
            
            # Test the controller by sending a neutral state
            self._send_neutral_state()
            
        except Exception as e:
            logger.error(f"[Gamepad] Error initializing controller: {e}")
            logger.error("[Gamepad] Make sure you have the proper drivers installed")
            logger.error("[Gamepad] On Windows, you may need to install ViGEm Bus Driver")
            raise RuntimeError(f"Failed to initialize virtual gamepad: {e}")

    def _send_neutral_state(self):
        """Send neutral state to ensure controller is working."""
        try:
            self.vgpad.left_joystick_float(x_value_float=0.0, y_value_float=0.0)
            self.vgpad.right_joystick_float(x_value_float=0.0, y_value_float=0.0)
            self.vgpad.left_trigger_float(value_float=0.0)
            self.vgpad.right_trigger_float(value_float=0.0)
            self.vgpad.update()
            logger.debug("[Gamepad] Neutral state sent successfully")
        except Exception as e:
            logger.error(f"[Gamepad] Error sending neutral state: {e}")
            raise

    def handle_input(self, input_type: int, idx: int, value: int) -> bool:
        """
        Process input from WebSocket and translate to virtual controller.
        Returns True if successful, False otherwise.
        """
        if not self.initialized or not self.vgpad:
            logger.error("[Gamepad] Controller not initialized, cannot process input")
            return False
        
        try:
            current_time = time.time()
            
            # Rate limiting to prevent excessive updates
            if current_time - self.last_update < self.update_threshold:
                return True
                
            # Validate input parameters
            if not self._validate_input(input_type, idx, value):
                return False
                
            success = False
            if input_type == 0:  # Handle Axes
                success = self._handle_axis_input(idx, value)
            elif input_type == 1:  # Handle Buttons
                success = self._handle_button_input(idx, value)
            
            if success:
                # Update the virtual gamepad
                self.vgpad.update()
                self.last_update = current_time
                return True
            else:
                return False
                
        except Exception as e:
            logger.error(f"[Gamepad] Error handling input: {e}")
            return False

    def _validate_input(self, input_type: int, idx: int, value: int) -> bool:
        """Validate input parameters."""
        if input_type not in [0, 1]:
            logger.warning(f"[Gamepad] Invalid input type: {input_type}")
            return False
        
        if input_type == 0 and (idx < 0 or idx > 5):  # Axis validation
            logger.warning(f"[Gamepad] Invalid axis index: {idx}")
            return False
        
        if input_type == 1 and (idx < 0 or idx > 13):  # Button validation
            logger.warning(f"[Gamepad] Invalid button index: {idx}")
            return False
        
        if not (-32768 <= value <= 32767):  # Value range validation
            logger.warning(f"[Gamepad] Invalid value: {value}")
            return False
        
        return True

    def _handle_axis_input(self, idx: int, value: int) -> bool:
        """Handle axis input (analog sticks and triggers)."""
        try:
            # Mapping of received axis IDs to internal names
            axis_map = {
                0: 'lx', 1: 'ly',  # Left Stick
                2: 'rx', 3: 'ry',  # Right Stick
                4: 'lt',           # Left Trigger (L2)
                5: 'rt'            # Right Trigger (R2)
            }
            
            if idx not in axis_map:
                logger.warning(f"[Gamepad] Unknown axis index: {idx}")
                return False
                
            axis_name = axis_map[idx]
            
            # Convert int16 to float (-1.0 to 1.0 for sticks, 0.0 to 1.0 for triggers)
            if axis_name in ['lt', 'rt']:  # Triggers are 0.0 to 1.0
                normalized_value = max(0.0, min(1.0, value / 32767.0))
            else:  # Analog sticks are -1.0 to 1.0
                normalized_value = max(-1.0, min(1.0, value / 32767.0))
            
            # Apply deadzone for analog sticks
            if axis_name in ['lx', 'ly', 'rx', 'ry']:
                deadzone = 0.08
                if abs(normalized_value) < deadzone:
                    normalized_value = 0.0
                else:
                    # Scale to account for deadzone
                    sign = 1 if normalized_value > 0 else -1
                    normalized_value = sign * (abs(normalized_value) - deadzone) / (1.0 - deadzone)
            
            # Only update if value changed significantly
            if abs(self.axes[axis_name] - normalized_value) < 0.001:
                return True  # Not an error, just no change needed
                
            self.axes[axis_name] = normalized_value

            # Update specific components of the virtual controller
            if axis_name in ['lx', 'ly']:  # Left stick
                self.vgpad.left_joystick_float(
                    x_value_float=self.axes['lx'], 
                    y_value_float=self.axes['ly']
                )
            elif axis_name in ['rx', 'ry']:  # Right stick
                self.vgpad.right_joystick_float(
                    x_value_float=self.axes['rx'], 
                    y_value_float=self.axes['ry']
                )
            elif axis_name == 'lt':  # Left trigger
                self.vgpad.left_trigger_float(value_float=normalized_value)
            elif axis_name == 'rt':  # Right trigger
                self.vgpad.right_trigger_float(value_float=normalized_value)
            
            return True
            
        except Exception as e:
            logger.error(f"[Gamepad] Error handling axis input: {e}")
            return False

    def _handle_button_input(self, idx: int, value: int) -> bool:
        """Handle button input."""
        try:
            # Mapping of received button IDs to vgamepad buttons
            button_mapping = {
                0: vg.XUSB_BUTTON.XUSB_GAMEPAD_A,
                1: vg.XUSB_BUTTON.XUSB_GAMEPAD_B,
                2: vg.XUSB_BUTTON.XUSB_GAMEPAD_X,
                3: vg.XUSB_BUTTON.XUSB_GAMEPAD_Y,
                4: vg.XUSB_BUTTON.XUSB_GAMEPAD_LEFT_SHOULDER,    # L1
                5: vg.XUSB_BUTTON.XUSB_GAMEPAD_RIGHT_SHOULDER,   # R1
                6: vg.XUSB_BUTTON.XUSB_GAMEPAD_BACK,             # Select/Share
                7: vg.XUSB_BUTTON.XUSB_GAMEPAD_START,            # Start/Options
                8: vg.XUSB_BUTTON.XUSB_GAMEPAD_LEFT_THUMB,       # L3
                9: vg.XUSB_BUTTON.XUSB_GAMEPAD_RIGHT_THUMB,      # R3
                10: vg.XUSB_BUTTON.XUSB_GAMEPAD_DPAD_UP,
                11: vg.XUSB_BUTTON.XUSB_GAMEPAD_DPAD_DOWN,
                12: vg.XUSB_BUTTON.XUSB_GAMEPAD_DPAD_LEFT,
                13: vg.XUSB_BUTTON.XUSB_GAMEPAD_DPAD_RIGHT,
            }
            
            if idx not in button_mapping:
                logger.warning(f"[Gamepad] Unknown button index: {idx}")
                return False
                
            button = button_mapping[idx]
            is_pressed = value == 1
            
            # Only update if button state changed
            if self.buttons_state.get(idx, False) == is_pressed:
                return True  # Not an error, just no change needed
                
            self.buttons_state[idx] = is_pressed
            
            if is_pressed:
                self.vgpad.press_button(button=button)
                logger.debug(f"[Gamepad] Button {idx} pressed")
            else:
                self.vgpad.release_button(button=button)
                logger.debug(f"[Gamepad] Button {idx} released")
            
            return True
            
        except Exception as e:
            logger.error(f"[Gamepad] Error handling button input: {e}")
            return False

    def reset(self) -> bool:
        """Reset all inputs to default state."""
        if not self.initialized or not self.vgpad:
            logger.error("[Gamepad] Controller not initialized, cannot reset")
            return False
        
        try:
            logger.info("[Gamepad] Resetting controller to neutral state...")
            
            # Reset all axes
            self.axes = {k: 0.0 for k in self.axes.keys()}
            self.vgpad.left_joystick_float(x_value_float=0.0, y_value_float=0.0)
            self.vgpad.right_joystick_float(x_value_float=0.0, y_value_float=0.0)
            self.vgpad.left_trigger_float(value_float=0.0)
            self.vgpad.right_trigger_float(value_float=0.0)
            
            # Reset all buttons
            button_mapping = {
                0: vg.XUSB_BUTTON.XUSB_GAMEPAD_A,
                1: vg.XUSB_BUTTON.XUSB_GAMEPAD_B,
                2: vg.XUSB_BUTTON.XUSB_GAMEPAD_X,
                3: vg.XUSB_BUTTON.XUSB_GAMEPAD_Y,
                4: vg.XUSB_BUTTON.XUSB_GAMEPAD_LEFT_SHOULDER,
                5: vg.XUSB_BUTTON.XUSB_GAMEPAD_RIGHT_SHOULDER,
                6: vg.XUSB_BUTTON.XUSB_GAMEPAD_BACK,
                7: vg.XUSB_BUTTON.XUSB_GAMEPAD_START,
                8: vg.XUSB_BUTTON.XUSB_GAMEPAD_LEFT_THUMB,
                9: vg.XUSB_BUTTON.XUSB_GAMEPAD_RIGHT_THUMB,
                10: vg.XUSB_BUTTON.XUSB_GAMEPAD_DPAD_UP,
                11: vg.XUSB_BUTTON.XUSB_GAMEPAD_DPAD_DOWN,
                12: vg.XUSB_BUTTON.XUSB_GAMEPAD_DPAD_LEFT,
                13: vg.XUSB_BUTTON.XUSB_GAMEPAD_DPAD_RIGHT,
            }
            
            for button_id, is_pressed in self.buttons_state.items():
                if is_pressed and button_id in button_mapping:
                    self.vgpad.release_button(button=button_mapping[button_id])
            
            self.buttons_state = {}
            self.vgpad.update()
            logger.info("[Gamepad] Controller reset to neutral state successfully")
            return True
            
        except Exception as e:
            logger.error(f"[Gamepad] Error resetting controller: {e}")
            return False

    def get_status(self) -> Dict:
        """Get current controller status."""
        return {
            "initialized": self.initialized,
            "axes": self.axes.copy(),
            "buttons_pressed": [k for k, v in self.buttons_state.items() if v],
            "last_update": self.last_update,
            "total_buttons": len(self.buttons_state),
            "vgamepad_available": VGAMEPAD_AVAILABLE
        }

    def __del__(self):
        """Cleanup when object is destroyed."""
        if self.initialized and self.vgpad:
            try:
                self.reset()
                logger.info("[Gamepad] Controller cleaned up successfully")
            except Exception as e:
                logger.error(f"[Gamepad] Error during cleanup: {e}")

# Test function for standalone testing
def test_gamepad():
    """Test the gamepad functionality."""
    try:
        logger.info("Testing gamepad initialization...")
        gamepad = Gamepad()
        
        if not gamepad.initialized:
            logger.error("Gamepad failed to initialize")
            return False
        
        logger.info("Testing axis input...")
        # Test left stick
        gamepad.handle_input(0, 0, 16384)  # Half right
        gamepad.handle_input(0, 1, -16384)  # Half up
        
        logger.info("Testing button input...")
        # Test A button
        gamepad.handle_input(1, 0, 1)  # Press A
        time.sleep(0.1)
        gamepad.handle_input(1, 0, 0)  # Release A
        
        logger.info("Testing reset...")
        gamepad.reset()
        
        logger.info("Gamepad test completed successfully")
        return True
        
    except Exception as e:
        logger.error(f"Gamepad test failed: {e}")
        return False

if __name__ == "__main__":
    # Setup logging for standalone testing
    logging.basicConfig(level=logging.INFO)
    
    # Run test
    if test_gamepad():
        print("Gamepad test passed!")
    else:
        print("Gamepad test failed!")
        sys.exit(1)