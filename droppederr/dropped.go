package droppederr

import (
	"fmt"
	"go/ast"
	"strings"
)

// scanDropped reports every `if err != nil { ... }` block inside fd whose
// body never references the error it guards.
func (c *checker) scanDropped(fd *ast.FuncDecl) {
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		ifStmt, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}

		c.reportDropped(fd.Name.Name, ifStmt)

		return true
	})
}

func (c *checker) reportDropped(function string, ifStmt *ast.IfStmt) {
	errVar, ok := errNilCheck(ifStmt.Cond)
	if !ok {
		return
	}

	if identUsed(ifStmt.Body, errVar) {
		return
	}

	body := render(c.pass.Fset, ifStmt.Body)
	detail := fmt.Sprintf("%s unused in %s", errVar, firstLine(body))

	c.pass.Reportf(ifStmt.Pos(), "%s: %s drops the root error: %s", classDropped, function, detail)
}

// identUsed reports whether node references an identifier named name
// anywhere within it.
func identUsed(node ast.Node, name string) bool {
	used := false

	ast.Inspect(node, func(n ast.Node) bool {
		ident, ok := n.(*ast.Ident)
		if !ok || ident.Name != name {
			return true
		}

		used = true

		return false
	})

	return used
}

// firstLine returns the first non-empty, non-brace line of s, truncated to
// 80 characters, for quoting a block's opening statement in a finding.
func firstLine(s string) string {
	for raw := range strings.SplitSeq(s, "\n") {
		line := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "{"))
		if line == "" || line == "}" {
			continue
		}

		if len(line) > 80 {
			line = line[:80] + "..."
		}

		return line
	}

	return "<empty block>"
}
