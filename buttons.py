#!/usr/bin/env python3
import os
os.environ["GPIOZERO_PIN_FACTORY"] = "lgpio"

import requests
import time
from gpiozero import Button, LED
from signal import pause

API_ACTION   = "http://localhost:8000/api/action"
API_SHUTDOWN = "http://localhost:8000/api/shutdown"

# (gpio_bouton, action, valeur, gpio_led ou None)
BUTTON_CONFIG = [
    (17, "score_home_inc",  0,   10),   # +1 score dom   LED MOSI
    (27, "score_home_dec",  0, None),   # -1 score dom
    (22, "fouls_home_inc",  0, None),   # +1 faute dom   (LED retirée, GPIO11 réaffecté ci-dessous)
    (24, "fouls_home_dec",  0, None),   # -1 faute dom
    (5,  "game_time_adj",  -1, None),   # chrono -1s
    (6,  "game_time_adj",   1, None),   # chrono +1s
    (12, "toggle_timer",    0,   23),   # start/stop     LED GPIO23
    (16, "reset_poss",      0,   21),   # reset poss     LED GPIO21
    (19, "poss_time_adj",  -1, None),   # poss -1s
    (18, "poss_time_adj",   1, None),   # poss +1s
    (20, "score_away_inc",  0,    9),   # +1 score vis   LED MISO
    (25, "score_away_dec",  0, None),   # -1 score vis
    (26, "fouls_away_inc",  0, None),   # +1 faute vis   (LED retirée, GPIO8 réaffecté ci-dessous)
    (4,  "fouls_away_dec",  0, None),   # -1 faute vis
    (11, "toggle_poss",     0, None),   # start/stop possession seule (ex-LED SCLK, faute+1 dom)
    (8,  "manual_horn",     0, None),   # buzzer manuel  (ex-LED CE0, faute+1 vis — celle qui restait légèrement allumée)
]

POWER_GPIO     = 7      # CE1
LONG_PRESS_SEC = 3.0

def send_action(action, val, led_device=None):
    try:
        if led_device:
            led_device.on()
        requests.post(API_ACTION, json={"action": action, "value": val}, timeout=0.5)
        if led_device:
            time.sleep(0.08)
            led_device.off()
    except Exception as e:
        print(f"Erreur action {action}: {e}")
        if led_device:
            led_device.off()

_power_press_time = 0.0

def on_power_press():
    global _power_press_time
    _power_press_time = time.monotonic()

def on_power_release():
    held = time.monotonic() - _power_press_time
    force = held >= LONG_PRESS_SEC
    print(f"Power {'long' if force else 'court'} — {'shutdown immédiat' if force else 'confirmation web'}")
    try:
        requests.post(API_SHUTDOWN, json={"force": force}, timeout=1.0)
    except Exception as e:
        print(f"Erreur shutdown: {e}")

buttons = []
for pin, action, val, led_pin in BUTTON_CONFIG:
    btn = Button(pin, pull_up=True, bounce_time=0.02)
    led = LED(led_pin) if led_pin else None
    btn.when_pressed = lambda a=action, v=val, l=led: send_action(a, v, l)
    buttons.append(btn)

power_btn = Button(POWER_GPIO, pull_up=True, bounce_time=0.05)
power_btn.when_pressed  = on_power_press
power_btn.when_released = on_power_release

print(f"Boutons actifs : {len(buttons)} jeu + 1 power (GPIO {POWER_GPIO})")
print(f"LEDs : GPIO 10 23 21 9 | Appui long {LONG_PRESS_SEC}s = shutdown direct")
pause()
