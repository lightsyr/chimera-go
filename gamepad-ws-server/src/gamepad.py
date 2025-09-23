import vgamepad as vg

class Gamepad:
    def __init__(self):
        """Inicializa o controle virtual de Xbox 360."""
        self.vgpad = vg.VX360Gamepad()
        # Armazena o último valor conhecido para cada eixo para evitar envios redundantes
        self.axes = {
            'lx': 0, 'ly': 0,  # Analógico Esquerdo (Left Stick)
            'rx': 0, 'ry': 0,  # Analógico Direito (Right Stick)
            'lt': 0, 'rt': 0   # Gatilhos (Left/Right Trigger)
        }
        print("[Gamepad] Controle virtual de Xbox 360 inicializado.")

    def handle_input(self, tipo, idx, valor):
        """
        Recebe o input do WebSocket e o traduz para o controle virtual.
        - tipo 0: Eixo (Analógicos e Gatilhos)
        - tipo 1: Botão (ABXY, D-Pad, Ombros, etc.)
        """
        if tipo == 0:  # Trata Eixos (Analógicos e Gatilhos)
            # Mapeamento de IDs de eixos recebidos para os nomes internos
            axis_map = {
                0: 'lx', 1: 'ly',  # Analógico Esquerdo
                2: 'rx', 3: 'ry',  # Analógico Direito
                4: 'lt',           # Gatilho Esquerdo (L2)
                5: 'rt'            # Gatilho Direito (R2)
            }
            if idx in axis_map:
                axis_name = axis_map[idx]
                self.axes[axis_name] = valor

                # Atualiza os componentes específicos do controle virtual
                if axis_name.startswith('l'):
                    self.vgpad.left_joystick_float(x_value_float=self.axes['lx'] / 32767.0, y_value_float=self.axes['ly'] / 32767.0)
                if axis_name.startswith('r'):
                    self.vgpad.right_joystick_float(x_value_float=self.axes['rx'] / 32767.0, y_value_float=self.axes['ry'] / 32767.0)
                
                # Gatilhos esperam um valor de 0 a 1.0
                if axis_name == 'lt':
                    self.vgpad.left_trigger_float(value_float=self.axes['lt'] / 32767.0)
                if axis_name == 'rt':
                    self.vgpad.right_trigger_float(value_float=self.axes['rt'] / 32767.0)

        elif tipo == 1:  # Trata Botões
            # Mapeamento dos IDs de botões recebidos para os botões do vgamepad
            button_mapping = {
                0: vg.XUSB_BUTTON.XUSB_GAMEPAD_A,
                1: vg.XUSB_BUTTON.XUSB_GAMEPAD_B,
                2: vg.XUSB_BUTTON.XUSB_GAMEPAD_X,
                3: vg.XUSB_BUTTON.XUSB_GAMEPAD_Y,
                4: vg.XUSB_BUTTON.XUSB_GAMEPAD_LEFT_SHOULDER,    # L1
                5: vg.XUSB_BUTTON.XUSB_GAMEPAD_RIGHT_SHOULDER,   # R1
                6: vg.XUSB_BUTTON.XUSB_GAMEPAD_BACK,             # Select / Share
                7: vg.XUSB_BUTTON.XUSB_GAMEPAD_START,            # Start / Options
                8: vg.XUSB_BUTTON.XUSB_GAMEPAD_LEFT_THUMB,     # L3
                9: vg.XUSB_BUTTON.XUSB_GAMEPAD_RIGHT_THUMB,    # R3
                10: vg.XUSB_BUTTON.XUSB_GAMEPAD_DPAD_UP,
                11: vg.XUSB_BUTTON.XUSB_GAMEPAD_DPAD_DOWN,
                12: vg.XUSB_BUTTON.XUSB_GAMEPAD_DPAD_LEFT,
                13: vg.XUSB_BUTTON.XUSB_GAMEPAD_DPAD_RIGHT,
            }
            if idx in button_mapping:
                button = button_mapping[idx]
                if valor == 1:
                    self.vgpad.press_button(button=button)
                else:
                    self.vgpad.release_button(button=button)

        # Envia todas as atualizações para o controle virtual de uma vez
        self.vgpad.update()