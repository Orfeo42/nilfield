package slogfmt

import (
	"bytes"
	"go/ast"
	"go/token"
	"strings"
)

// slogKVFuncs maps slog function names to the number of leading positional
// arguments that precede their variadic key-value pairs.
var slogKVFuncs = map[string]int{
	"Debug":        1, // slog.Debug(msg, args...)
	"Info":         1,
	"Warn":         1,
	"Error":        1,
	"DebugContext": 2, // slog.DebugContext(ctx, msg, args...)
	"InfoContext":  2,
	"WarnContext":  2,
	"ErrorContext": 2,
	"Log":          3, // slog.Log(ctx, level, msg, args...)
}

// slogCallKVStart returns the index at which key-value pairs start for a
// slog.Xxx call, and whether call is a slog call at all.
func slogCallKVStart(call *ast.CallExpr) (int, bool) {
	sel, isSel := call.Fun.(*ast.SelectorExpr)
	if !isSel {
		return 0, false
	}

	pkg, isPkg := sel.X.(*ast.Ident)
	if !isPkg || pkg.Name != "slog" {
		return 0, false
	}

	kvStart, ok := slogKVFuncs[sel.Sel.Name]

	return kvStart, ok
}

// rewriteCall rebuilds the source of a slog call so that its fixed leading
// arguments stay on the first line and each key-value pair occupies one line:
//
//	slog.Error("msg", "k1", v1, "k2", v2)
//
// becomes:
//
//	slog.Error("msg",
//		"k1", v1,
//		"k2", v2,
//	)
//
// For context variants the ctx argument stays on the first line too:
//
//	slog.ErrorContext(ctx, "msg",
//		"k1", v1,
//	)
func rewriteCall(src []byte, call *ast.CallExpr, fset *token.FileSet) ([]byte, error) {
	tf := fset.File(call.Pos())
	kvStart, _ := slogCallKVStart(call)

	indent := lineIndent(src, tf.Offset(call.Pos()))
	innerIndent := indent + "\t"

	argSrcs := make([][]byte, len(call.Args))
	for i, arg := range call.Args {
		start := tf.Offset(arg.Pos())
		end := tf.Offset(arg.End())
		argSrcs[i] = bytes.TrimSpace(src[start:end])
	}

	funSrc := src[tf.Offset(call.Fun.Pos()):tf.Offset(call.Fun.End())]

	var buf bytes.Buffer

	buf.Write(funSrc)
	buf.WriteByte('(')

	// Leading fixed args are comma-separated with a space, EXCEPT after the
	// last one: that comma is immediately followed by the '\n' below, and a
	// trailing space there would survive into the rewritten call (the
	// original standalone tool always ran a final go/format.Source pass over
	// the whole file, which silently stripped this; an analyzer's suggested
	// fix has no such pass, so the text must be clean on its own).
	for i := 0; i < kvStart; i++ {
		buf.Write(argSrcs[i])
		buf.WriteByte(',')

		if i < kvStart-1 {
			buf.WriteByte(' ')
		}
	}

	kvArgs := argSrcs[kvStart:]
	if len(kvArgs) == 0 {
		buf.WriteByte(')')

		return buf.Bytes(), nil
	}

	buf.WriteByte('\n')

	for i := 0; i < len(kvArgs); i += 2 {
		buf.WriteString(innerIndent)
		buf.Write(kvArgs[i])

		if i+1 < len(kvArgs) {
			buf.WriteString(", ")
			buf.Write(kvArgs[i+1])
		}

		buf.WriteString(",\n")
	}

	buf.WriteString(indent)
	buf.WriteByte(')')

	return buf.Bytes(), nil
}

// lineIndent returns the leading run of tabs and spaces on the source line
// containing byte offset off.
func lineIndent(src []byte, off int) string {
	lineStart := off
	for lineStart > 0 && src[lineStart-1] != '\n' {
		lineStart--
	}

	var indent strings.Builder

	for _, b := range src[lineStart:off] {
		if b != '\t' && b != ' ' {
			break
		}

		indent.WriteByte(b)
	}

	return indent.String()
}
