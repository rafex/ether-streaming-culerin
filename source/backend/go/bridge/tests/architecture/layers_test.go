// Package architecture enforces the hexagonal dependency rule (ARCH-0001):
// domain → nothing internal; ports → domain; application → domain, ports;
// infrastructure → anything but other adapters' internals; nobody inside
// imports cmd/.
package architecture

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const module = "github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/"

var allowed = map[string][]string{
	"internal/domain":      {},
	"internal/ports":       {"internal/domain"},
	"internal/application": {"internal/domain", "internal/ports"},
}

// thirdParty lists external modules a layer may not use (the core stays pure).
var forbiddenExternal = map[string][]string{
	"internal/domain":      {"github.com/", "golang.org/"},
	"internal/ports":       {"github.com/", "golang.org/"},
	"internal/application": {"github.com/", "golang.org/"},
}

func TestHexagonalLayers(t *testing.T) {
	root := filepath.Join("..", "..")
	err := filepath.Walk(filepath.Join(root, "internal"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		rel, _ := filepath.Rel(root, filepath.Dir(path))
		rel = filepath.ToSlash(rel)
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range file.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if strings.HasPrefix(p, module+"cmd") {
				t.Errorf("%s imports %s (cmd is the composition root)", path, p)
			}
			layer, rules := layerOf(rel)
			if layer == "" {
				continue
			}
			if strings.HasPrefix(p, module) {
				target := strings.TrimPrefix(p, module)
				ok := strings.HasPrefix(target, layer)
				for _, a := range rules {
					ok = ok || strings.HasPrefix(target, a)
				}
				if !ok {
					t.Errorf("%s (%s) must not import %s", path, layer, target)
				}
				continue
			}
			for _, prefix := range forbiddenExternal[layer] {
				if strings.HasPrefix(p, prefix) {
					t.Errorf("%s (%s) must not depend on %s", path, layer, p)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func layerOf(rel string) (string, []string) {
	for layer, rules := range allowed {
		if rel == layer || strings.HasPrefix(rel, layer+"/") {
			return layer, rules
		}
	}
	return "", nil
}
