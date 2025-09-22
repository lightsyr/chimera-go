import pygame

class Gamepad:
    def __init__(self):
        pygame.init()
        self.gamepad = None
        self.connected = False
        self.connect_gamepad()

    def connect_gamepad(self):
        for i in range(pygame.joystick.get_count()):
            self.gamepad = pygame.joystick.Joystick(i)
            self.gamepad.init()
            self.connected = True
            break

    def get_inputs(self):
        if not self.connected:
            return None

        pygame.event.pump()
        inputs = {
            'axes': [self.gamepad.get_axis(i) for i in range(self.gamepad.get_numaxes())],
            'buttons': [self.gamepad.get_button(i) for i in range(self.gamepad.get_numbuttons())],
        }
        return inputs

    def close(self):
        if self.connected:
            self.gamepad.quit()
            pygame.quit()