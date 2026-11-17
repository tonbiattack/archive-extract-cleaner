package archive

import (
	"go/ast"
	"go/parser"
	"go/token"
	"golang.org/x/sys/windows"
	"testing"
)

// Both process creation sites must configure the command before it is started.
func TestSevenZipCommandsHideConsole(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "sevenzip.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Open", "listSevenZip"} {
		t.Run(name, func(t *testing.T) {
			found := false
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Name.Name != name {
					continue
				}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					id, ok := call.Fun.(*ast.Ident)
					if ok && id.Name == "hideSevenZipConsole" && len(call.Args) == 1 {
						arg, ok := call.Args[0].(*ast.Ident)
						found = ok && arg.Name == "cmd"
					}
					return true
				})
			}
			if !found {
				t.Error("7-Zip command is not configured to hide its console")
			}
		})
	}
}

func TestSevenZipExtractionWindowsFlags(t *testing.T) {
	path := sevenZipFixture(t, "7z")
	a, err := Open(path, testLimits())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	entry := a.Variants()[0].Files["a.txt"]
	r, err := a.Open(entry)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	cmd := r.(*commandReader).cmd
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.CreationFlags&windows.CREATE_NO_WINDOW == 0 {
		t.Fatal("7-Zip extraction must use CREATE_NO_WINDOW")
	}
}
