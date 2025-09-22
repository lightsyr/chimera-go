import vgamepad as vg

class Gamepad:
    def __init__(self):
        self.vgpad = vg.VX360Gamepad()
        self.lx = 0
        self.ly = 0
        self.rx = 0
        self.ry = 0

    def handle_input(self, tipo, idx, valor):
        if tipo == 0:  # Analógico
            if idx == 0: self.lx = valor
            if idx == 1: self.ly = valor
            if idx == 2: self.rx = valor
            if idx == 3: self.ry = valor
            self.vgpad.left_joystick(x_value=self.lx, y_value=self.ly)
            self.vgpad.right_joystick(x_value=self.rx, y_value=self.ry)

        elif tipo == 1:  # Botão
            mapping = {
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
            if idx in mapping:
                if valor == 1:
                    self.vgpad.press_button(mapping[idx])
                else:
                    self.vgpad.release_button(mapping[idx])

        elif tipo == 2:  # Gatilho
            if idx == 0:
                self.vgpad.left_trigger(value=valor >> 7)  # escala para 0–255
            elif idx == 1:
                self.vgpad.right_trigger(value=valor >> 7)

        self.vgpad.update()
