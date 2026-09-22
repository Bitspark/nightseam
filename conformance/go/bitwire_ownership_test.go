package conformance

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The public access contract has one owner in every language. This catches
// aliases as well as copied definitions, which interoperability tests alone
// cannot distinguish from using the upstream contract directly.
func TestBitwireOwnsSharedDeclarations(t *testing.T) {
	root := filepath.Join("..", "..")
	cmd := exec.Command("git", "ls-files", "duplex", "runtime", "tunnel", "live")
	cmd.Dir = root
	files, err := cmd.Output()
	if err != nil { t.Fatal(err) }
	declaration := regexp.MustCompile(`(?m)^\s*(?:(?:pub|public|export|final|struct|abstract)\s+)*(?:type|typealias|interface|protocol|class|struct|trait|enum|data|newtype)\s+(?:Wire|Endpoint|Message|Receiver|ReturnAddress|ProfileFrame|ProfileKind|ProfileError)\b`)
	for _, name := range strings.Fields(string(files)) {
		if strings.Contains(name, "/test") || strings.Contains(name, "/Tests/") || strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, ".test.ts") { continue }
		ext := filepath.Ext(name)
		if ext != ".go" && ext != ".ts" && ext != ".py" && ext != ".rs" && ext != ".hpp" && ext != ".java" && ext != ".swift" && ext != ".hs" { continue }
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil { t.Fatal(err) }
		// Rust's private socket state happens to be called Wire; it is not
		// part of the public access surface.
		for _, match := range declaration.FindAllString(string(data), -1) {
			t.Errorf("%s redeclares Bitwire's contract: %s", name, strings.TrimSpace(match))
		}
	}
}
