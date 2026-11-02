import fcntl, json, os, pathlib, pty, select, struct, subprocess, tempfile, termios, time
binary = str(pathlib.Path(__file__).resolve().parents[1] / 'bin' / 'reverse-proxy')

def read_for(master, seconds):
    data = b''
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        if select.select([master], [], [], .05)[0]:
            try: part = os.read(master, 65536)
            except OSError: break
            if not part: break
            data += part
    return data.decode('utf-8', errors='replace')

with tempfile.TemporaryDirectory(prefix='relay-terminal-check-') as folder:
    master, slave = pty.openpty()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack('HHHH',36,120,0,0))
    logs = pathlib.Path(folder,'requests.jsonl')
    process = subprocess.Popen([binary,'-demo','-listen','127.0.0.1:0','-log-file',str(logs)],stdin=slave,stdout=slave,stderr=slave,env={**os.environ,'TERM':'xterm-256color'})
    os.close(slave)
    try:
        assert 'HEALTHY TRAFFIC' in read_for(master,.8)
        fcntl.ioctl(master,termios.TIOCSWINSZ,struct.pack('HHHH',28,76,0,0))
        assert '[f/r/b/a] demo controls' in read_for(master,.5)
        fcntl.ioctl(master,termios.TIOCSWINSZ,struct.pack('HHHH',20,60,0,0))
        assert 'Resize' in read_for(master,.5)
        fcntl.ioctl(master,termios.TIOCSWINSZ,struct.pack('HHHH',36,120,0,0))
        assert 'RECENT REQUESTS' in read_for(master,.5)
        for key, expected in [('f','UPSTREAM OUTAGE'),('r','RECOVERY'),('b','TRAFFIC BURST'),('a','HEALTHY TRAFFIC'),('e','ERRORS ONLY'),('e','ALL REQUESTS'),(' ','DISPLAY FROZEN'),(' ','LOCAL DEMO')]:
            os.write(master,key.encode())
            assert expected in read_for(master,.4), (key,expected)
        os.write(master,b'q')
        tail = read_for(master,.6)
        assert process.wait(timeout=12) == 0
        assert '\x1b[?1049l' in tail and '\x1b[?25h' in tail
        events = [json.loads(line) for line in logs.read_text().splitlines()]
        requests = [e for e in events if e['msg']=='request completed']
        assert requests and any(e['status']==200 for e in requests)
        assert logs.stat().st_mode & 0o777 == 0o600
        print('PASS real PTY controls, compact layout, resize recovery, terminal restoration, and private JSON log output')
    finally:
        if process.poll() is None: process.kill(); process.wait()
        os.close(master)
result = subprocess.run([binary,'-ui'],capture_output=True,text=True)
assert result.returncode == 1 and 'interactive terminal' in result.stderr
print('PASS non-interactive dashboard invocation exits with a useful error')
