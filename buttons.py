#!/usr/bin/env python3
"""Boutons physiques du tableau 3x3 (GPIO, Raspberry Pi 5 / lgpio).

Chaque bouton envoie une action au serveur Go via /api/action. Le bouton
power envoie une demande d'extinction : appui court = confirmation à l'écran,
appui long (>= 3 s) = extinction immédiate.
"""
import os
os.environ["GPIOZERO_PIN_FACTORY"] = "lgpio"

import threading
import time

import requests
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

# Les boutons ±1 s répètent l'action tant qu'on les maintient : plus besoin
# de marteler 20 fois pour corriger le chrono de 20 secondes.
REPEAT_ACTIONS = {"game_time_adj", "poss_time_adj"}
HOLD_REPEAT_SEC = 0.4   # délai avant la 1re répétition, puis entre deux répétitions

HTTP_TIMEOUT = 1.5      # s — large pour qu'un Pi chargé ne perde pas d'appui
LED_FLASH_SEC = 0.08

# Une session HTTP par thread : la connexion TCP est réutilisée (appui → effet
# plus rapide, pas de nouvelle connexion à chaque bouton) sans partager d'état
# entre les threads de callbacks de gpiozero.
_local = threading.local()


def session():
    s = getattr(_local, "session", None)
    if s is None:
        s = requests.Session()
        _local.session = s
    return s


def post(url, payload):
    """POST JSON avec une relance si la connexion réutilisée est périmée."""
    for attempt in (1, 2):
        try:
            session().post(url, json=payload, timeout=HTTP_TIMEOUT)
            return True
        except Exception as e:
            _local.session = None  # repartir d'une connexion neuve
            if attempt == 2:
                print(f"Erreur POST {url} {payload}: {e}", flush=True)
    return False


def send_action(action, val, led_device=None):
    if led_device:
        led_device.blink(on_time=LED_FLASH_SEC, off_time=0, n=1, background=True)
    post(API_ACTION, {"action": action, "value": val})


_power_press_time = None


def on_power_press():
    global _power_press_time
    _power_press_time = time.monotonic()


def on_power_release():
    global _power_press_time
    if _power_press_time is None:   # relâchement sans appui préalable (rebond)
        return
    held = time.monotonic() - _power_press_time
    _power_press_time = None
    force = held >= LONG_PRESS_SEC
    print(f"Power {'long' if force else 'court'} — {'shutdown immédiat' if force else 'confirmation web'}", flush=True)
    post(API_SHUTDOWN, {"force": force})


buttons = []
for pin, action, val, led_pin in BUTTON_CONFIG:
    led = LED(led_pin) if led_pin else None
    handler = lambda a=action, v=val, l=led: send_action(a, v, l)
    if action in REPEAT_ACTIONS:
        btn = Button(pin, pull_up=True, bounce_time=0.02, hold_time=HOLD_REPEAT_SEC, hold_repeat=True)
        btn.when_held = handler
    else:
        btn = Button(pin, pull_up=True, bounce_time=0.02)
    btn.when_pressed = handler
    buttons.append(btn)

power_btn = Button(POWER_GPIO, pull_up=True, bounce_time=0.05)
power_btn.when_pressed  = on_power_press
power_btn.when_released = on_power_release

print(f"Boutons actifs : {len(buttons)} jeu + 1 power (GPIO {POWER_GPIO})", flush=True)
print(f"LEDs : GPIO 10 23 21 9 | Appui long {LONG_PRESS_SEC}s = shutdown direct", flush=True)
pause()
