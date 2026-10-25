package main

import (
	"bufio"
	"bytes"
	"cmp"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	markerFile    = ".planviewer-demo"
	planviewerPkg = "github.com/nbr23/planviewer"
	serveAddr     = "127.0.0.1:8080"
)

var launchers = []struct {
	label string
	args  []string
}{
	{"TUI", nil},
	{"web server on http://" + serveAddr, []string{"serve", "-addr", serveAddr}},
}

//go:embed content
var content embed.FS

type demo struct {
	out, home string
	now       time.Time
}

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/home/dev"
	}
	out := flag.String("out", filepath.Join(os.TempDir(), "planviewer-demo"), "directory to write the fake Claude Code, Codex and Antigravity data to; replaced if an earlier run wrote it")
	fakeHome := flag.String("home", home, "home directory the fake projects live under; resume commands show ~/src/<project> when it is $HOME")
	flag.Parse()

	dir, err := filepath.Abs(*out)
	if err != nil {
		fail(err)
	}
	d := demo{out: dir, home: filepath.Clean(*fakeHome), now: time.Now().UTC().Truncate(time.Second)}
	if err := d.run(); err != nil {
		fail(err)
	}
	fmt.Printf("wrote demo data to %s\n\n", d.out)
	if args, ok := choose(); ok {
		fail(launch(d.command(args)))
	}
	for _, l := range launchers {
		fmt.Println("  " + strings.Join(d.command(l.args), " "))
	}
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "demo: %v\n", err)
	os.Exit(1)
}

func choose() ([]string, bool) {
	if info, err := os.Stdin.Stat(); err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return nil, false
	}
	fmt.Println("Open it in planviewer:")
	for i, l := range launchers {
		fmt.Printf("  %d) %s\n", i+1, l.label)
	}
	fmt.Printf("  %d) neither, print the commands\n", len(launchers)+1)
	in := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("choice [1]: ")
		if !in.Scan() {
			fmt.Println()
			return nil, false
		}
		answer := cmp.Or(strings.TrimSpace(in.Text()), "1")
		n, err := strconv.Atoi(answer)
		switch {
		case err != nil || n < 1 || n > len(launchers)+1:
			continue
		case n == len(launchers)+1:
			fmt.Println()
			return nil, false
		}
		return launchers[n-1].args, true
	}
}

func (d demo) command(args []string) []string {
	argv := append([]string{"go", "run", planviewerPkg}, args...)
	return append(argv, "-claude-dir", d.claudeDir(), "-codex-dir", d.codexDir(), "-agy-dir", d.geminiDir(), "-cache-dir=")
}

func launch(argv []string) error {
	bin, err := exec.LookPath(argv[0])
	if err != nil {
		return err
	}
	fmt.Println("$ " + strings.Join(argv, " "))
	return syscall.Exec(bin, argv, os.Environ())
}

func (d demo) run() error {
	if err := d.clear(); err != nil {
		return err
	}
	for _, write := range []func() error{d.writeClaude, d.writeCodex, d.writeAgy} {
		if err := write(); err != nil {
			return err
		}
	}
	return writeFile(filepath.Join(d.out, markerFile), nil)
}

func (d demo) clear() error {
	entries, err := os.ReadDir(d.out)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return err
	case len(entries) == 0:
		return nil
	}
	if _, err := os.Stat(filepath.Join(d.out, markerFile)); err != nil {
		return fmt.Errorf("%s is not empty and was not written by this generator; choose another -out", d.out)
	}
	return os.RemoveAll(d.out)
}

func (d demo) at(ago time.Duration) time.Time { return d.now.Add(-ago) }

func (d demo) cwd(project string) string { return filepath.Join(d.home, "src", project) }

func (d demo) claudeDir() string { return filepath.Join(d.out, ".claude") }

func (d demo) codexDir() string { return filepath.Join(d.out, ".codex") }

func (d demo) geminiDir() string { return filepath.Join(d.out, ".gemini") }

func body(key string) string {
	b, err := content.ReadFile("content/" + key)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func writeFile(p string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

func writeStamped(p string, data []byte, at time.Time) error {
	if err := writeFile(p, data); err != nil {
		return err
	}
	return os.Chtimes(p, at, at)
}

func appendJSON(buf *bytes.Buffer, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	buf.Write(b)
	buf.WriteByte('\n')
}
