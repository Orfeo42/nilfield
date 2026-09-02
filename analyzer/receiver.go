package analyzer

import (
	"go/ast"
	"go/types"
)

// nilSafeReceiver marks a pointer-receiver method whose body never dereferences
// its own receiver, so a call through a value already known to be nil is not
// itself a hazard: the method tolerates it.
type nilSafeReceiver struct{}

func (*nilSafeReceiver) AFact() {}

func (*nilSafeReceiver) String() string { return "nil-safe receiver" }

// exportNilSafeReceiverFact records, for a named-pointer-receiver method fd,
// whether its body ever uses the receiver in a way that would panic if the
// receiver were nil. It probes the body with a separate silent checker so the
// probe's own traversal reports no diagnostics and exports no facts of its own.
// It returns whether it exported a fact that was not already recorded.
func (c *checker) exportNilSafeReceiverFact(fd *ast.FuncDecl) bool {
	if fd.Body == nil {
		return false
	}

	recv, ok := receiverName(fd)
	if !ok {
		return false
	}

	fn, isFunc := c.pass.TypesInfo.ObjectOf(fd.Name).(*types.Func)
	if !isFunc || !c.isPointerReceiver(fn) {
		return false
	}

	var scratch nilSafeReceiver
	if c.pass.ImportObjectFact(fn, &scratch) {
		return false
	}

	probe := &checker{
		pass:         c.pass,
		excludePaths: c.excludePaths,
		walked:       map[*ast.FuncLit]bool{},
		resolve:      c.resolve,
		silent:       true,
		receiver:     recv,
		sig:          c.declSignature(fd),
	}

	probe.walk(fd.Body.List, newScope())

	if probe.receiverDeref {
		return false
	}

	c.pass.ExportObjectFact(fn, &nilSafeReceiver{})

	return true
}

// isPointerReceiver reports whether fn is declared with a pointer receiver.
func (c *checker) isPointerReceiver(fn *types.Func) bool {
	sig := fn.Signature()
	if sig == nil || sig.Recv() == nil {
		return false
	}

	_, isPointer := types.Unalias(sig.Recv().Type()).Underlying().(*types.Pointer)

	return isPointer
}
