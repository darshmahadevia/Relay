#!/usr/bin/env python3
"""Record Relay in a PTY and export its terminal output as an MP4."""

import argparse
import codecs
import fcntl
import json
import os
from pathlib import Path
import pty
import re
import select
import shutil
import struct
import subprocess
import termios
import time

import pyte
from PIL import Image, ImageDraw, ImageFont


def capture(binary, duration, output):
    columns, rows = 120, 36
    master, slave = pty.openpty()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", rows, columns, 0, 0))
    process = subprocess.Popen(
        [str(binary), "-demo", "-listen", "127.0.0.1:0"],
        stdin=slave, stdout=slave, stderr=slave,
        env={**{key: value for key, value in os.environ.items() if key != "NO_COLOR"}, "TERM": "xterm-256color", "COLORTERM": "truecolor"},
        start_new_session=True,
    )
    os.close(slave)
    started = time.monotonic()
    events = []
    decoder = codecs.getincrementaldecoder("utf-8")()
    actions = [(3.5, " "), (4.5, " "), (10, "e"), (12, "e")]
    try:
        while time.monotonic() - started < duration:
            elapsed = time.monotonic() - started
            while actions and elapsed >= actions[0][0]:
                _, key = actions.pop(0)
                os.write(master, key.encode())
                events.append([elapsed, "i", key])
            if select.select([master], [], [], .05)[0]:
                try:
                    data = os.read(master, 65536)
                except OSError:
                    break
                if not data:
                    break
                events.append([time.monotonic() - started, "o", decoder.decode(data)])
            if process.poll() is not None:
                raise RuntimeError(f"Relay exited early with status {process.returncode}")
        os.write(master, b"q")
        process.wait(timeout=12)
        if process.returncode:
            raise RuntimeError(f"Relay exited with status {process.returncode}")
    finally:
        if process.poll() is None:
            process.kill()
            process.wait()
        os.close(master)
    with output.open("w") as file:
        file.write(json.dumps({"version": 2, "width": columns, "height": rows, "title": "Relay | live HTTP proxy demo", "env": {"TERM": "xterm-256color"}}) + "\n")
        for event in events:
            file.write(json.dumps(event, ensure_ascii=False) + "\n")
    return events, columns, rows


def font_path():
    for candidate in [
        "/System/Library/Fonts/Menlo.ttc",
        "/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
        "/usr/share/fonts/truetype/liberation2/LiberationMono-Regular.ttf",
    ]:
        if Path(candidate).exists():
            return candidate
    raise RuntimeError("Install Menlo, DejaVu Sans Mono, or Liberation Mono")


def export(events, columns, rows, duration, output):
    cell_width, cell_height = 11, 24
    width, height = columns * cell_width + 80, rows * cell_height + 88
    font = ImageFont.truetype(font_path(), 18)
    screen = pyte.Screen(columns, rows)
    stream = pyte.Stream(screen)
    fps = 10
    encoder = subprocess.Popen([
        "ffmpeg", "-y", "-loglevel", "error", "-f", "rawvideo", "-pixel_format", "rgb24",
        "-video_size", f"{width}x{height}", "-framerate", str(fps), "-i", "pipe:0",
        "-an", "-c:v", "libx264", "-preset", "medium", "-crf", "20",
        "-pix_fmt", "yuv420p", "-movflags", "+faststart", str(output),
    ], stdin=subprocess.PIPE)
    observed = set()
    position = 0
    poster_written = False
    try:
        for frame_number in range(int(duration * fps)):
            at = frame_number / fps
            while position < len(events) and events[position][0] <= at:
                _, kind, data = events[position]
                if kind == "o":
                    stream.feed(data)
                position += 1
            text = "\n".join(screen.display)
            observed.update(re.findall(r"CIRCUIT\s+(CLOSED|HALF-OPEN|OPEN)\b", text))
            for state in ["TRAFFIC BURST", "ERRORS ONLY", "DISPLAY FROZEN"]:
                if state in text:
                    observed.add(state)
            image = Image.new("RGB", (width, height), "#080c10")
            draw = ImageDraw.Draw(image)
            draw.rounded_rectangle((20, 16, width - 20, height - 16), radius=12, fill="#0b1015", outline="#34434b")
            draw.text((40, 28), "relay  /  localhost demonstration", font=font, fill="#91a4b0")
            draw.line((20, 58, width - 20, 58), fill="#34434b")
            for row in range(rows):
                col = 0
                while col < columns:
                    char = screen.buffer[row][col]
                    fg, bg = char.fg, char.bg
                    end = col + 1
                    while end < columns:
                        next_char = screen.buffer[row][end]
                        if next_char.fg != fg or next_char.bg != bg:
                            break
                        end += 1
                    value = "".join(screen.buffer[row][i].data or " " for i in range(col, end))
                    x, y = 40 + col * cell_width, 66 + row * cell_height
                    color = "#" + fg if len(fg) == 6 else "#dce6e8"
                    if len(bg) == 6:
                        draw.rectangle((x, y, x + (end - col) * cell_width, y + cell_height), fill="#" + bg)
                    draw.text((x, y), value, font=font, fill=color)
                    col = end
            encoder.stdin.write(image.tobytes())
            if at >= 27 and not poster_written:
                image.save(output.with_name("relay-dashboard.png"))
                poster_written = True
    finally:
        encoder.stdin.close()
        if encoder.wait() != 0:
            raise RuntimeError("ffmpeg failed")
    if duration >= 32:
        required = {"CLOSED", "OPEN", "HALF-OPEN", "TRAFFIC BURST", "ERRORS ONLY", "DISPLAY FROZEN"}
        if required - observed:
            raise RuntimeError(f"Recording missed expected dashboard states: {required - observed}")
    print("Verified recorded states:", ", ".join(sorted(observed)))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--duration", type=float, default=36)
    args = parser.parse_args()
    if not 1 <= args.duration <= 120:
        parser.error("duration must be between 1 and 120 seconds")
    if not shutil.which("ffmpeg"):
        parser.error("ffmpeg is required to export the recording")
    root = Path(__file__).resolve().parents[1]
    binary = root / "bin" / "reverse-proxy"
    if not binary.exists():
        parser.error("build Relay first with make build")
    output = root / "docs" / "relay-demo.mp4"
    output.parent.mkdir(exist_ok=True)
    print(f"Recording {args.duration:g} seconds of real terminal output", flush=True)
    events, columns, rows = capture(binary, args.duration, output.with_suffix(".cast"))
    print("Exporting terminal recording to MP4", flush=True)
    export(events, columns, rows, args.duration, output)
    print(output)


if __name__ == "__main__":
    main()
