#!/usr/bin/env python3
"""Generate See's screenshots: capture the real app, driven through MyGo's
automation protocol, in scenes seeded through the workspace session.

Subcommands:
  fixture DIR          create the fixture project at DIR
  capture --see BIN --out DIR [--scenes a,b] [--home DIR]
                       run the scenes and write PNGs
  compose-hero --screen PNG --window PNG --bounds X,Y,W,H --out PNG
                       crop the full-screen capture around the window

The app needs the automation plugin: build with `-tags e2e` and launch
with MYGO_AUTOMATION set, which `capture` does itself.
"""

import argparse
import json
import os
import shutil
import socket
import subprocess
import sys
import tempfile
import time
import base64


# ---------------------------------------------------------------- fixture

MAIN_GO = '''package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"pulse/internal/pipe"
	"pulse/internal/store"
)

// main wires the pipeline: read metrics off stdin, aggregate them into
// the store, and flush a summary every interval or on SIGTERM.
func main() {
	st := store.New(256)
	p := pipe.New(st,
		pipe.WithWindow(30),
		pipe.WithLanes(4),
	)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sig
		fmt.Println(st.Summary())
		os.Exit(0)
	}()

	if err := p.Run(os.Stdin); err != nil {
		fmt.Fprintln(os.Stderr, "pulse:", err)
		os.Exit(1)
	}
}
'''

PIPE_GO = '''// Package pipe moves metric samples from a reader into the store,
// spreading the work over a fixed number of lanes.
package pipe

import (
	"bufio"
	"fmt"
	"io"
	"sync"

	"pulse/internal/store"
)

// A Pipe reads samples, parses them, and hands them to the store.
type Pipe struct {
	store  *store.Store
	window int
	lanes  int
}

// An Option tunes a Pipe's behavior.
type Option func(*Pipe)

// WithWindow sets the aggregation window, in seconds.
func WithWindow(seconds int) Option {
	return func(p *Pipe) { p.window = seconds }
}

// WithLanes sets how many lanes share the parsing work.
func WithLanes(n int) Option {
	return func(p *Pipe) { p.lanes = n }
}

// New returns a Pipe writing into st.
func New(st *store.Store, opts ...Option) *Pipe {
	p := &Pipe{store: st, window: 10, lanes: 2}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Run reads samples from r until it is exhausted or a line fails to
// parse. Each lane owns a shard of the metric names, so samples of one
// name never race.
func (p *Pipe) Run(r io.Reader) error {
	samples := make(chan string, p.lanes*64)
	var wg sync.WaitGroup
	for lane := 0; lane < p.lanes; lane++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for line := range samples {
				name, value, ok := parse(line)
				if ok {
					p.store.Add(name, value)
				}
			}
		}()
	}

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		samples <- scanner.Text()
	}
	close(samples)
	wg.Wait()
	return scanner.Err()
}

func parse(line string) (string, float64, bool) {
	var name string
	var value float64
	n, err := fmt.Sscanf(line, "%s %f", &name, &value)
	return name, value, n == 2 && err == nil
}
'''

STORE_GO = '''// Package store keeps the most recent samples of every metric and
// summarizes them on demand.
package store

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// A Store is a bounded set of per-metric rings.
type Store struct {
	mu    sync.Mutex
	cap   int
	rings map[string]*ring
}

// New returns a Store keeping at most cap samples per metric.
func New(capacity int) *Store {
	return &Store{cap: capacity, rings: map[string]*ring{}}
}

// Add records one sample of the metric.
func (s *Store) Add(name string, value float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rings[name]
	if !ok {
		r = newRing(s.cap)
		s.rings[name] = r
	}
	r.push(value)
}

// Summary formats every metric's mean and peak, ordered by name.
func (s *Store) Summary() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.rings))
	for name := range s.rings {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	for _, name := range names {
		r := s.rings[name]
		fmt.Fprintf(&b, "%-24s mean %8.2f  peak %8.2f  n %d\\n",
			name, r.mean(), r.peak(), r.len())
	}
	return b.String()
}
'''

RING_GO = '''package store

// ring is a bounded circular buffer of samples, zero allocation once
// full: the oldest sample is overwritten by the newest.
type ring struct {
	buf  []float64
	head int
	n    int
}

func newRing(capacity int) *ring {
	return &ring{buf: make([]float64, capacity)}
}

func (r *ring) push(v float64) {
	r.buf[r.head%len(r.buf)] = v
	r.head++
	if r.n < len(r.buf) {
		r.n++
	}
}

func (r *ring) len() int { return r.n }

func (r *ring) mean() float64 {
	if r.n == 0 {
		return 0
	}
	var sum float64
	for i := 0; i < r.n; i++ {
		sum += r.buf[i]
	}
	return sum / float64(r.n)
}

func (r *ring) peak() float64 {
	var peak float64
	for i := 0; i < r.n; i++ {
		if r.buf[i] > peak {
			peak = r.buf[i]
		}
	}
	return peak
}
'''

PIPE_TEST_GO = '''package pipe

import (
	"strings"
	"testing"

	"pulse/internal/store"
)

func TestRunAggregatesSamples(t *testing.T) {
	st := store.New(16)
	p := New(st, WithLanes(2))
	input := strings.NewReader("cpu 0.4\\ncpu 0.9\\nmem 12.5\\n")
	if err := p.Run(input); err != nil {
		t.Fatal(err)
	}
	summary := st.Summary()
	if !strings.Contains(summary, "cpu") || !strings.Contains(summary, "mem") {
		t.Fatalf("summary misses metrics: %q", summary)
	}
}

func TestParseRejectsNoise(t *testing.T) {
	if _, _, ok := parse("not-a-metric"); ok {
		t.Fatal("parse accepted a sample without a value")
	}
	name, value, ok := parse("cpu 0.75")
	if !ok || name != "cpu" || value != 0.75 {
		t.Fatalf("parse = %q %v %v", name, value, ok)
	}
}
'''

README_MD = '''# pulse

A tiny metrics pipeline: samples in, rolling summaries out.

```
$ printf 'cpu 0.4\\ncpu 0.9\\n' | pulse
cpu                      mean     0.65  peak     0.90  n 2
```

## Why

- **Zero allocation** once the rings fill up; the oldest sample is
  overwritten in place.
- **Lane-sharded** parsing, so samples of one metric never race.
- **Bounded memory** — every metric keeps at most *N* samples.

## Usage

| Flag | Default | Meaning |
| --- | --- | --- |
| `-window` | `10` | aggregation window, seconds |
| `-lanes` | `2` | parsing lanes |

Pipe samples as `name value` lines on standard input. Send `SIGTERM`
for the final summary.

## Hacking

1. `go test ./...` runs the suite.
2. `go run .` starts a pipeline on stdin.

The layout follows the standard Go project structure: the binary in
`main.go`, the pipeline in `internal/pipe`, storage in `internal/store`.
'''

GO_MOD = '''module pulse

go 1.23
'''

FIXTURE_PNG = base64.b64decode(
    "iVBORw0KGgoAAAANSUhEUgAAAAgAAAAICAYAAADED76LAAAAHklEQVQYlWP8"
    "z8AABIwM1P8fA4lKBv//MwEABcwCHeT8t+EAAAAASUVORK5CYII=")

MAIN_V1 = """package main

import "fmt"

func main() {
\tfmt.Println("pulse: nothing here yet")
}
"""

# store.go before the summary was ordered: the metrics print in map
# order, which the final commit fixes.
STORE_UNSORTED = STORE_GO.replace(
    """\tnames := make([]string, 0, len(s.rings))
\tfor name := range s.rings {
\t\tnames = append(names, name)
\t}
\tsort.Strings(names)

""", """\tnames := make([]string, 0, len(s.rings))
\tfor name := range s.rings {
\t\tnames = append(names, name)
\t}

""").replace('\t"sort"\n', "")


def git(root, *args, env_extra=None):
    env = dict(os.environ)
    env.update({
        "GIT_AUTHOR_NAME": "Rui Lin",
        "GIT_AUTHOR_EMAIL": "rui@example.com",
        "GIT_COMMITTER_NAME": "Rui Lin",
        "GIT_COMMITTER_EMAIL": "rui@example.com",
    })
    if env_extra:
        env.update(env_extra)
    subprocess.run(["git", "-C", root] + list(args), check=True, env=env,
                   stdout=subprocess.DEVNULL)


def write(root, rel, content):
    path = os.path.join(root, rel)
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        f.write(content)


# The fixture's history as (message, {path: content}) pairs, oldest
# first: every commit carries the real work its message claims, so the
# log, the diffs and the blame all read as a project's.
LANE_GO = """package pipe

// A lane parses one share of the samples, in lockstep with the rest.
type lane struct {
	input  chan string
	parsed chan sample
}

type sample struct {
	name  string
	value float64
}
"""

BUCKET_GO = """package store

// A bucket is one second of aggregated samples, the unit the window
// drops as it slides.
type bucket struct {
	second int64
	counts map[string]int
	sums   map[string]float64
}

func newBucket(second int64) *bucket {
	return &bucket{second: second, counts: map[string]int{}, sums: map[string]float64{}}
}
"""


def history_versions():
    return [
        ("Initial commit: pulse skeleton", {
            "main.go": MAIN_V1,
            "go.mod": GO_MOD,
            ".gitignore": "/pulse\n",
            "Makefile": "test:\n\tgo test ./...\n",
            "LICENSE": "MIT License\n",
            "docs/guide.md": "# Guide\n\nPulse reads samples off stdin.\n",
            "docs/hacking.md": "# Hacking\n\nRun `go test ./...`.\n",
            "testdata/samples.txt": "cpu 0.4\nmem 0.6\n",
            "scripts/check.sh": "#!/bin/sh\ngo vet ./...\n",
        }),
        ("pipe: spread parsing over lanes", {
            "main.go": MAIN_GO,
            "internal/pipe/pipe.go": PIPE_GO,
            "internal/pipe/lane.go": LANE_GO,
        }),
        ("store: keep per-metric rings bounded", {
            "internal/store/store.go": STORE_UNSORTED,
            "internal/store/ring.go": RING_GO,
        }),
        ("readme: document the pipeline and flags", {
            "README.md": README_MD,
        }),
        ("pipe: reject samples without values", {
            "internal/pipe/pipe_test.go": PIPE_TEST_GO,
        }),
        ("store: order the summary by metric name", {
            "internal/store/store.go": STORE_GO,
            "internal/store/bucket.go": BUCKET_GO,
        }),
    ]


def fixture(root):
    """Create the fixture project at root: a small Go module with a git
    history, a tag, and uncommitted changes."""
    os.makedirs(root, exist_ok=True)
    os.makedirs(os.path.join(root, "assets"), exist_ok=True)
    with open(os.path.join(root, "assets/icon.png"), "wb") as f:
        f.write(FIXTURE_PNG)

    git(root, "init", "-q", "-b", "main")
    dates = [
        "2026-11-14T09:30:00", "2026-11-15T14:12:00", "2026-11-17T11:00:00",
        "2026-11-18T16:45:00", "2026-11-19T10:20:00", "2026-11-20T15:05:00",
    ]
    for i, (message, files) in enumerate(history_versions()):
        for rel, content in files.items():
            write(root, rel, content)
        git(root, "add", "-A")
        git(root, "commit", "-q", "-m", message,
            env_extra={"GIT_AUTHOR_DATE": dates[i], "GIT_COMMITTER_DATE": dates[i]})
    git(root, "tag", "v0.2.0")
    git(root, "branch", "feature/alerts")

    # Working tree changes for the Git navigator and the diff: a staged
    # edit, an unstaged edit, and an untracked file.
    write(root, "internal/pipe/pipe.go", PIPE_GO.replace(
        "samples := make(chan string, p.lanes*64)",
        "samples := make(chan string, p.lanes*128)"))
    write(root, "internal/store/store.go", STORE_GO.replace(
        "%-24s mean", "%-28s mean"))
    write(root, "internal/pipe/alerts.go",
          "package pipe\n\n// WIP: alert rules evaluated over the window.\n")
    git(root, "add", "internal/store/store.go")
    # The compiled binary, for the hex view.
    subprocess.run(["go", "build", "-o", "pulse", "."], cwd=root,
                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)



# ---------------------------------------------------------------- protocol

class Automation:
    """A client of the automation protocol over its Unix socket."""

    def __init__(self, path, timeout=30):
        deadline = time.time() + timeout
        while True:
            try:
                self.sock = socket.socket(socket.AF_UNIX)
                self.sock.connect(path)
                break
            except OSError:
                if time.time() > deadline:
                    raise
                time.sleep(0.2)
        self.buf = b""
        self.next_id = 0

    def rpc(self, method, params=None, timeout=30):
        self.next_id += 1
        rid = self.next_id
        self.sock.sendall((json.dumps(
            {"id": rid, "method": method, "params": params or {}}) + "\n").encode())
        self.sock.settimeout(timeout)
        while True:
            while b"\n" not in self.buf:
                chunk = self.sock.recv(1 << 20)
                if not chunk:
                    raise RuntimeError("automation socket closed")
                self.buf += chunk
            line, self.buf = self.buf.split(b"\n", 1)
            answer = json.loads(line)
            if answer.get("id") != rid:
                continue
            if "error" in answer:
                raise RuntimeError(f"{method}: {answer['error']}")
            return answer.get("result", {})

    def windows(self):
        return self.rpc("windows")["windows"]

    def idle(self, timeout=10):
        try:
            self.rpc("idle", {"timeout": timeout * 1000}, timeout=timeout + 5)
        except RuntimeError as e:
            if "timeout" not in str(e):
                raise

    def capture(self, path, window=None):
        params = {}
        if window is not None:
            params["window"] = window
        result = self.rpc("capture", params, timeout=60)
        data = base64.b64decode(result["png"])
        with open(path, "wb") as f:
            f.write(data)
        return result["width"], result["height"]

    def key(self, key, window=None):
        self.input_events([{"type": "key", "key": key}], window)

    def text(self, text, window=None):
        self.input_events([{"type": "text", "text": text}], window)

    def click(self, x, y, window=None):
        self.input_events([{"type": "click", "x": x, "y": y}], window)

    def input_events(self, events, window=None):
        params = {"events": events}
        if window is not None:
            params["window"] = window
        self.rpc("input", params)

    def act(self, key, action, window=None):
        params = {"key": key, "action": action}
        if window is not None:
            params["window"] = window
        self.rpc("act", params)


def config_dir(home):
    if sys.platform == "darwin":
        return os.path.join(home, "Library", "Application Support", "See")
    return os.path.join(home, ".config", "See")


# ---------------------------------------------------------------- scenes

def hero_session(root, width=1440, height=860):
    """The hero composition, dense as a reader's screen really is: three
    editor columns — a Go file, its pipeline, the compiled binary as hex
    — the readme's rendered markdown among the tabs, the terminal open
    below, the regex playground floating over it all."""
    main = os.path.join(root, "main.go")
    pipe = os.path.join(root, "internal/pipe/pipe.go")
    store = os.path.join(root, "internal/store/store.go")
    readme = os.path.join(root, "README.md")
    binary = os.path.join(root, "pulse")
    return {
        "windowWidth": width,
        "windowHeight": height,
        "panelOpen": True,
        "panelPercent": 74,
        "terminals": [{"panes": [{"dir": root, "title": "zsh"}]}],
        "regexToolX": width * 0.36,
        "regexToolY": 96,
        "regexToolWidth": 470,
        "regexToolHeight": 500,
        "layout": {
            "vertical": False,
            "percent": 50,
            "first": {
                "tabs": [{"path": main, "line": 14, "column": 1},
                         {"path": store},
                         {"path": pipe}],
                "active": main,
                "focused": True,
            },
            "second": {
                "tabs": [{"path": readme, "previewTab": True},
                         {"path": binary}],
                "active": binary,
            },
        },
    }


def diff_session(root):
    """The unstaged change of pipe.go open as a diff, the Git navigator
    listing the working tree."""
    return {
        "windowWidth": 1440,
        "windowHeight": 900,
        "navigator": 2,  # the Git navigator
        "layout": {
            "tabs": [{"diff": {"path": "internal/pipe/pipe.go"}}],
            "active": "\u0000diff:false:internal/pipe/pipe.go",
            "focused": True,
        },
    }


def plain_session(root):
    return {
        "windowWidth": 1440,
        "windowHeight": 900,
        "layout": {
            "tabs": [{"path": os.path.join(root, "internal/pipe/pipe.go")}],
            "active": os.path.join(root, "internal/pipe/pipe.go"),
            "focused": True,
        },
    }


# Steps run after the app settled. `settle` waits for the UI to go idle,
# `palette` runs a command through the command palette.
def palette(client, text):
    client.key("CmdOrCtrl+Shift+P")
    client.idle()
    client.text(text)
    client.idle()
    client.key("Enter")
    client.idle()


def regex_steps(client):
    palette(client, "Test Regex")
    time.sleep(0.8)
    if sys.platform == "darwin":
        place_regex_window()
    client.text("read 3 samples\nwrite err once\ndone")
    time.sleep(0.3)
    for _ in range(4):
        client.key("Shift+Tab")
        time.sleep(0.15)
    client.text("err|done")
    time.sleep(0.5)


def terminal_steps(client):
    """Type `go test ./...` into the terminal and run it: the hero's
    panel shows real output rather than an empty prompt. The click lands
    in the bottom panel, whose geometry the seeded session fixes."""
    client.click(720, 800)
    client.idle()
    client.text("go test ./... && git log --graph --oneline --all --decorate")
    client.idle()
    client.key("Enter")
    time.sleep(5)  # the run's output settles
    client.idle()


SCENES = {
    "hero-light": {
        "appearance": "light",
        "session": hero_session,
        "steps": [("terminal", None)],
    },
    "hero-dark": {
        "appearance": "dark",
        "session": hero_session,
        "steps": [("terminal", None)],
    },
    "diff": {
        "appearance": "light",
        "session": diff_session,
        "steps": [],
    },
    "gitlog": {
        "appearance": "light",
        "session": lambda root: {
            "windowWidth": 1440, "windowHeight": 900,
            "navigator": 2,
            "layout": plain_session(root)["layout"],
        },
        "steps": [("palette", "Git: Show Log")],
    },
}


NEOFETCH = """#!/bin/sh
# A neofetch-style card: the terminal in the shots is never empty.
GREEN='\033[32m'; CYAN='\033[36m'; BOLD='\033[1m'; RESET='\033[0m'
printf "${GREEN}                    'c.                  ${RESET}${BOLD}wibus@macbook-pro${RESET}\n"
printf "${GREEN}                 ,xNMM.                  ${RESET}${CYAN}--------------${RESET}\n"
printf "${GREEN}               .OMMMMo                   ${CYAN}OS${RESET}       macOS 15.7 arm64\n"
printf "${GREEN}               OMMM0,                    ${CYAN}Host${RESET}     MacBook Pro 14-inch M4 Pro\n"
printf "${GREEN}     .;loddo:' loolloddol;.              ${CYAN}Kernel${RESET}   24.6.0\n"
printf "${GREEN}   cKMMMMMMMMMMNWMMMMMMMMMM0:            ${CYAN}Shell${RESET}    zsh 5.9\n"
printf "${GREEN} .KMMMMMMMMMMMMMMMMMMMMMMMWd.            ${CYAN}Editor${RESET}   See\n"
printf "${GREEN} XMMMMMMMMMMMMMMMMMMMMMMMX.              ${CYAN}Uptime${RESET}   3 days, 4 hours\n"
printf "${GREEN};MMMMMMMMMMMMMMMMMMMMMMMM:              \n"
printf "${GREEN}:MMMMMMMMMMMMMMMMMMMMMMMM:              \n"
printf "${GREEN}.MMMMMMMMMMMMMMMMMMMMMMMX.              \n"
printf "${GREEN} kMMMMMMMMMMMMMMMMMMMMMMMMWd.           \n"
printf "${GREEN} .XMMMMMMMMMMMMMMMMMMMMMMMMMMk          \n"
printf "${GREEN}  .XMMMMMMMMMMMMMMMMMMMMMMMMK.          \n"
printf "${GREEN}    kMMMMMMMMMMMMMMMMMMMMMMd            \n"
printf "${GREEN}     ;KMMMMMMMWXXWMMMMMMMk.             \n"
printf "${GREEN}       .cooc,.    .,coo:.               ${RESET}\n"
"""


def write_config(home, root, scene):
    # zsh in the panel, with a prompt of just the directory: the runner's
    # long hostname and the bash migration notice stay out of the shot.
    with open(os.path.join(home, ".zshrc"), "w") as f:
        f.write("export PATH=\"$HOME/bin:$PATH\"\nPROMPT='%1~ %# '\n")
    with open(os.path.join(home, ".bashrc"), "w") as f:
        f.write("export PATH=\"$HOME/bin:$PATH\"\nPS1='\\W \\# '\n")
    bin_dir = os.path.join(home, "bin")
    os.makedirs(bin_dir, exist_ok=True)
    nf = os.path.join(bin_dir, "nf")
    with open(nf, "w") as f:
        f.write(NEOFETCH)
    os.chmod(nf, 0o755)
    cfg = {
        "terminalShell": "/bin/zsh" if os.path.exists("/bin/zsh") else "/bin/bash",
        "appearance": scene.get("appearance", ""),
        "trustedWorkspaces": [root],
        "hiddenNames": ".DS_Store,.git",
        "workspaceSessions": {root: scene["session"](root)
                              if callable(scene["session"]) else scene["session"]},
    }
    d = config_dir(home)
    os.makedirs(d, exist_ok=True)
    with open(os.path.join(d, "config.json"), "w") as f:
        json.dump(cfg, f)


# The dock is about 75 DIPs tall here, plus a few pixels of clearance:
# the screen's usable bottom for both centering and cropping.
DOCK = 82


def center_window():
    """macOS opens the window wherever it likes; the wallpaper crop wants
    the same room on every side, so center it in the usable screen: below
    the menu bar, above the dock."""
    x = int((1920 - 1440) / 2)
    y = int(25 + (1080 - 25 - DOCK - 860) / 2)
    for proc in ("See", "see-e2e", "see"):
        script = (f'tell application "System Events" to set position of window 1 '
                  f'of process "{proc}" to {{{x}, {y}}}')
        r = subprocess.run(["osascript", "-e", script],
                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        if r.returncode == 0:
            break


def place_regex_window():
    """The playground is the process's floating window: compact, inside
    the editor area over the first columns' seam, the editors and the
    terminal visible around it."""
    x, y, w, h = 480, 210, 440, 470
    for proc in ("See", "see-e2e", "see"):
        script = (f'tell application "System Events" to set position of window 2 '
                  f'of process "{proc}" to {{{x}, {y}}}')
        r = subprocess.run(["osascript", "-e", script],
                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        script = (f'tell application "System Events" to set size of window 2 '
                  f'of process "{proc}" to {{{w}, {h}}}')
        subprocess.run(["osascript", "-e", script],
                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        if r.returncode == 0:
            return


def run_scene(see, name, scene, out_dir, work, screencapture=False):
    home = os.path.join(work, "home")
    root = os.path.join(home, "dev", "pulse")
    fixture(root)
    os.makedirs(home, exist_ok=True)
    write_config(home, root, scene)

    sock = os.path.join(work, "see.sock")
    env = dict(os.environ)
    env["MYGO_AUTOMATION"] = sock
    env["HOME"] = home
    env.setdefault("DISPLAY", ":0")
    # X authority is looked up in HOME; point it back at the real one.
    if sys.platform != "darwin":
        env["XAUTHORITY"] = os.path.expanduser("~/.Xauthority")
    # A warm build cache: the terminal's `go test` finishes in a second.
    gocache = os.path.join(tempfile.gettempdir(), "see-shots-gocache")
    env["GOCACHE"] = gocache
    subprocess.run(["go", "test", "./..."], cwd=root, env=env,
                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    proc = subprocess.Popen(
        [see, "--see-cli-open", root],
        env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        client = Automation(sock, timeout=30)
        # The socket listens before the app's windows exist.
        for _ in range(100):
            if client.windows():
                break
            time.sleep(0.2)
        else:
            raise RuntimeError("the app showed no window")
        client.idle(timeout=10)
        if sys.platform == "darwin":
            center_window()
        time.sleep(1.5)  # the navigator, git status, and the panel catch up
        client.idle(timeout=10)
        for step in scene["steps"]:
            kind, arg = step
            if kind == "palette":
                palette(client, arg)
            elif kind == "regex":
                regex_steps(client)
            elif kind == "text":
                client.text(arg)
                time.sleep(0.5)
            elif kind == "terminal":
                terminal_steps(client)
        time.sleep(1.0)
        client.idle(timeout=10)
        out = os.path.join(out_dir, name + ".png")
        width, height = client.capture(out)
        print(f"{name}: {out} ({width}x{height})")
        # The window's screen bounds and the capture's scale, for the
        # hero crop: the PNG's pixels per DIP.
        for w in client.windows():
            if w["visible"] and w["bounds"]["width"] > 1000:
                png_width = png_pixel_width(out)
                scale = png_width / width if width else 1
                meta = os.path.join(out_dir, name + ".json")
                with open(meta, "w") as f:
                    json.dump({"bounds": w["bounds"], "captureWidth": width,
                               "captureHeight": height, "scale": scale}, f)
                if screencapture:
                    screen = os.path.join(out_dir, name + "-screen.png")
                    try:
                        subprocess.run(["screencapture", "-x", screen], check=True)
                    except (OSError, subprocess.CalledProcessError) as e:
                        print(f"{name}: screencapture failed: {e}; the automation capture stands")
                        continue
                    compose_hero(screen, meta, os.path.join(
                        out_dir, name + "-native.png"), margin_dips=0)
                    compose_hero(screen, meta, os.path.join(
                        out_dir, name + "-wallpaper.png"))
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()


def capture(see, out_dir, names, screencapture=False):
    os.makedirs(out_dir, exist_ok=True)
    for name in names:
        scene = SCENES[name]
        with tempfile.TemporaryDirectory() as work:
            run_scene(see, name, scene, out_dir, work, screencapture)


# ---------------------------------------------------------------- hero crop

def png_pixel_width(path):
    with open(path, "rb") as f:
        header = f.read(24)
    import struct
    return struct.unpack(">I", header[16:20])[0]


def compose_hero(screen_png, meta_json, out_png, margin_dips=110):
    """Crop the full-screen capture around the window, keeping a margin of
    wallpaper. Scale is the screenshot's pixels per DIP, as the window
    capture's."""
    try:
        from PIL import Image
    except ImportError:
        sys.exit("compose-hero needs Pillow: pip install pillow")
    with open(meta_json) as f:
        meta = json.load(f)
    b = meta["bounds"]
    scale = meta.get("scale", 1)
    img = Image.open(screen_png)
    x, y = int(b["x"] * scale), int(b["y"] * scale)
    x2, y2 = int((b["x"] + b["width"]) * scale), int((b["y"] + b["height"]) * scale)
    if margin_dips:
        # The same wallpaper margin on all four sides: the tightest side
        # decides, never crossing the menu bar or the screen's edges.
        want = int(margin_dips * scale)
        menubar = int(25 * scale)
        dock = int(DOCK * scale)
        # The wallpaper gap is equal on every side; the bottom edge then
        # extends past the dock, so the dock stands inside the bottom
        # margin rather than eating it.
        m = max(0, min(want, x, y - menubar, img.width - x2))
    else:
        m = 0
        dock = 0
    x0, y0 = x - m, y - m
    x1 = x2 + m
    y1 = min(img.height, y2 + m + dock) if margin_dips else y2 + m
    img.crop((x0, y0, x1, y1)).save(out_png)
    print(f"hero: {out_png} ({x1 - x0}x{y1 - y0})")


def wallpaper(out_png, width=1920, height=1080):
    """A macOS-like desktop picture: soft color blobs over a deep blue
    ground, heavily blurred. Generated rather than copied — macOS's own
    wallpapers are not ours to ship."""
    try:
        from PIL import Image, ImageDraw, ImageFilter
    except ImportError:
        sys.exit("wallpaper needs Pillow: pip install pillow")
    small = Image.new("RGB", (24, 14))
    px = small.load()
    ground = (38, 54, 92)
    for yy in range(14):
        for xx in range(24):
            px[xx, yy] = ground
    blobs = [
        (5, 3, 9, (240, 149, 115)),   # warm peach, as Sonoma's dawn
        (16, 2, 8, (190, 110, 160)),  # rose
        (19, 10, 10, (90, 110, 200)),  # violet blue
        (8, 11, 9, (70, 140, 190)),   # teal blue
    ]
    layer = Image.new("RGB", (24, 14), ground)
    draw = ImageDraw.Draw(layer)
    for cx, cy, r, color in blobs:
        draw.ellipse([cx - r, cy - r, cx + r, cy + r], fill=color)
    layer = layer.filter(ImageFilter.GaussianBlur(6))
    img = layer.resize((width, height), Image.BICUBIC).filter(ImageFilter.GaussianBlur(30))
    img.save(out_png)
    print(f"wallpaper: {out_png}")


def main():
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = parser.add_subparsers(dest="cmd", required=True)
    p = sub.add_parser("fixture")
    p.add_argument("dir")
    p = sub.add_parser("capture")
    p.add_argument("--see", required=True)
    p.add_argument("--out", required=True)
    p.add_argument("--scenes", default=",".join(SCENES))
    p.add_argument("--screencapture", action="store_true",
                   help="screencapture the screen after every scene, cropping the window and the hero's wallpaper shot")
    p = sub.add_parser("wallpaper")
    p.add_argument("out")
    p = sub.add_parser("compose-hero")
    p.add_argument("--screen", required=True)
    p.add_argument("--meta", required=True)
    p.add_argument("--out", required=True)
    args = parser.parse_args()

    if args.cmd == "wallpaper":
        wallpaper(args.out)
    elif args.cmd == "fixture":
        fixture(args.dir)
    elif args.cmd == "capture":
        capture(args.see, args.out, args.scenes.split(","), args.screencapture)
    elif args.cmd == "compose-hero":
        compose_hero(args.screen, args.meta, args.out)


if __name__ == "__main__":
    main()
