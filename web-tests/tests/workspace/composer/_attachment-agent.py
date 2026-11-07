import base64
import json
import os
import sys
import termios
import threading
import tty

mode, log_path = sys.argv[1:]
original = termios.tcgetattr(0)
lock = threading.Lock()
timers = []
state = {'paste': False, 'text': b'', 'draft': '', 'ready': False, 'enters': 0, 'submits': 0}


def record(event, **fields):
    with open(log_path, 'a') as output:
        output.write(json.dumps({'event': event, **fields}) + '\n')


def draw():
    os.write(1, ('\x1b[2J\x1b[HFAKE_IMAGE_AGENT\r\n❯\u00a0' + state['draft']).encode())


def ready():
    with lock:
        state['draft'] = '[Image #1]' if mode == 'image-marker' else state['text'].decode()
        state['ready'] = True
        record('image_ready')
        draw()


tty.setraw(0)
try:
    os.write(1, b'\x1b[?2004h')
    draw()
    record('started')
    pending = b''
    while True:
        data = os.read(0, 4096)
        if not data or b'\x04' in data:
            break
        record('bytes', value=base64.b64encode(data).decode())
        pending += data
        with lock:
            while pending:
                if not state['paste'] and b'\x1b[200~'.startswith(pending) and len(pending) < 6:
                    break
                if pending.startswith(b'\x1b[200~'):
                    pending = pending[6:]
                    state.update(paste=True, text=b'', ready=False, enters=0)
                elif state['paste']:
                    end = pending.find(b'\x1b[201~')
                    if end < 0:
                        # Keep a possible split closing marker for the next PTY read.
                        if len(pending) > 6:
                            state['text'] += pending[:-6]
                            pending = pending[-6:]
                        break
                    state['text'] += pending[:end]
                    pending = pending[end + 6:]
                    state['paste'] = False
                    record('paste', value=state['text'].decode())
                    if b'./.context/' in state['text']:
                        state['draft'] = ''
                        draw()
                        os.write(1, b'\r\nPasting\xe2\x80\xa6')
                        timer = threading.Timer(1 if mode == 'cancel' else 0.35, ready)
                        timers.append(timer)
                        timer.start()
                    else:
                        state.update(draft=state['text'].decode(), ready=True)
                        draw()
                elif pending.startswith(b'\r'):
                    pending = pending[1:]
                    state['enters'] += 1
                    if not state['ready']:
                        record('early_cr')
                    elif mode == 'swallow-all' or (mode == 'swallow-first' and state['enters'] == 1):
                        record('ignored_cr')
                        draw()
                    else:
                        state['submits'] += 1
                        record('submitted', count=state['submits'], text=state['draft'])
                        if mode != 'stale-echo':
                            state.update(draft='', ready=False)
                            draw()
                else:
                    record('unexpected', value=base64.b64encode(pending).decode())
                    pending = b''
finally:
    for timer in timers:
        timer.cancel()
    os.write(1, b'\x1b[?2004l')
    termios.tcsetattr(0, termios.TCSADRAIN, original)
    record('stopped')
