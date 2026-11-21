package netcheck

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Which of our packages may import which network packages.
var allowed = map[string][]string{
	"net/http":   {"internal/oauth"},
	"net/smtp":   {"internal/send"},
	"crypto/tls": {"internal/imapsync", "internal/send"},
	"net":        {"internal/imapsync", "internal/send", "internal/oauth", "internal/ipc", "internal/provider"},
}

var networkPkgs = []string{"net/http", "net/smtp", "net/rpc", "crypto/tls", "net/mail", "net"}

func root(t *testing.T) string {
	d, _ := os.Getwd()
	return filepath.Clean(filepath.Join(d, "..", ".."))
}

func TestNetworkImports(t *testing.T) {
	r := root(t)
	fset := token.NewFileSet()
	for _, top := range []string{"internal", "cmd"} {
		filepath.WalkDir(filepath.Join(r, top), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			rel, _ := filepath.Rel(r, filepath.Dir(p))
			f, err := parser.ParseFile(fset, p, nil, parser.ImportsOnly)
			if err != nil {
				t.Errorf("%s: %v", p, err)
				return nil
			}
			for _, im := range f.Imports {
				path, _ := strconv.Unquote(im.Path.Value)
				if path == "net/mail" || path == "net/url" || path == "net/netip" {
					continue // parsing only
				}
				if !isNetwork(path) {
					continue
				}
				ok := false
				for _, pkg := range allowed[base(path)] {
					if rel == pkg {
						ok = true
					}
				}
				if !ok {
					t.Errorf("%s imports %s; only %v may", rel, path, allowed[base(path)])
				}
			}
			return nil
		})
	}
}

func isNetwork(p string) bool {
	for _, n := range networkPkgs {
		if p == n || strings.HasPrefix(p, n+"/") {
			return true
		}
	}
	return false
}

func base(p string) string {
	for _, n := range []string{"net/http", "net/smtp", "net/rpc", "crypto/tls"} {
		if p == n || strings.HasPrefix(p, n+"/") {
			return n
		}
	}
	return p
}

// The socket package only ever opens Unix sockets.
func TestIPCIsUnixOnly(t *testing.T) {
	r := root(t)
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.Join(r, "internal/ipc/ipc.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == "net" && strings.HasPrefix(sel.Sel.Name, "Dial") || ok && id.Name == "net" && strings.HasPrefix(sel.Sel.Name, "Listen") {
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Value != `"unix"` {
				t.Errorf("%s: net.%s must use \"unix\"", fset.Position(call.Pos()), sel.Sel.Name)
			}
		}
		return true
	})
}

// No third-party dependency may speak HTTP (only our OAuth code does).
func TestDependenciesDontSpeakHTTP(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go tool")
	}
	cmd := exec.Command("go", "list", "-deps", "-f", `{{if not .Standard}}{{.ImportPath}} {{join .Imports " "}}{{end}}`, "./cmd/...")
	cmd.Dir = root(t)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "github.com/jamalxcode/lasermail/") {
			continue
		}
		for _, im := range f[1:] {
			if im == "net/http" || strings.HasPrefix(im, "net/http/") || im == "net/rpc" {
				t.Errorf("dependency %s imports %s", f[0], im)
			}
		}
	}
}

// The app shell has no network code: it talks to laserd over the socket,
// and its web view blocks every load. The one exception is the updater
// helper (its own executable, run only on Check for Updates…), which may talk
// to GitHub and nothing else.
func TestAppHasNoNetworkCode(t *testing.T) {
	r := root(t)
	var swift int
	sawRules := false
	updater := filepath.Join(r, "app", "Sources", "LaserEmailUpdater") + string(filepath.Separator)
	filepath.WalkDir(filepath.Join(r, "app"), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".swift") {
			return nil
		}
		if strings.HasPrefix(p, updater) {
			b, _ := os.ReadFile(p)
			for _, m := range regexp.MustCompile(`https://[^"/\\ ]+`).FindAllString(string(b), -1) {
				host := strings.TrimPrefix(m, "https://")
				if host != "github.com" && host != "objects.githubusercontent.com" && host != "release-assets.githubusercontent.com" {
					t.Errorf("%s names %s; the updater may reach GitHub only", p, m)
				}
			}
			return nil
		}
		swift++
		b, _ := os.ReadFile(p)
		s := string(b)
		for _, bad := range []string{"URLSession", "import Network", "NSURLConnection", "CFNetwork", "NWConnection", "URLRequest("} {
			if strings.Contains(s, bad) {
				t.Errorf("%s uses %s", p, bad)
			}
		}
		if strings.Contains(s, "WKContentRuleListStore") {
			sawRules = true
		}
		return nil
	})
	if swift > 0 && !sawRules {
		t.Error("the message view must install a WKContentRuleList that blocks remote loads")
	}
}
