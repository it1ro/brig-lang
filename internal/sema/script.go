package sema

import "github.com/it1ro/brig-lang/internal/ast"

// ScriptMainMessage — текст info о `fn main()` в script (§11.3, T-243).
const ScriptMainMessage = "script defines fn main() but never calls it; add module Main to run it"

// CheckScriptMain — info для script-файла (§11.3): top-level `fn main()`
// есть, а ссылок на `main` нет. В script main сама не вызывается, и такой
// файл почти наверняка задуман как модуль Main. prog — весь script
// (parser.ModeScript). info не влияет на exit code (§E.3).
func CheckScriptMain(prog *ast.Program) *Result {
	res := &Result{}
	if prog == nil {
		return res
	}
	var decl ast.Node
	for _, d := range prog.Decls {
		if fd, ok := d.(ast.FuncDecl); ok && fd.FnName() == "main" && decl == nil {
			decl = fd
		}
	}
	for _, s := range prog.Stmts {
		if fd, ok := s.(ast.LocalFnDecl); ok && fd.FnName() == "main" && decl == nil {
			decl = fd
		}
	}
	if decl == nil {
		return res
	}
	refs := &mainRefs{}
	if err := ast.Walk(refs, prog); err != nil || refs.found {
		return res
	}
	line, col := posOf(decl)
	res.Diagnostics = append(res.Diagnostics, Diagnostic{
		Line: line, Col: col, Severity: SeverityInfo, Message: ScriptMainMessage,
	})
	return res
}

// mainRefs ищет ссылку на имя `main`: вызов или значение (`spawn(main)`).
type mainRefs struct{ found bool }

func (r *mainRefs) VisitNode(ast.Node) error       { return nil }
func (r *mainRefs) VisitStmt(ast.Stmt) error       { return nil }
func (r *mainRefs) VisitPattern(ast.Pattern) error { return nil }
func (r *mainRefs) VisitType(ast.Type) error       { return nil }
func (r *mainRefs) VisitDecl(ast.Decl) error       { return nil }

func (r *mainRefs) VisitExpr(e ast.Expr) error {
	switch x := e.(type) {
	case ast.VariableExpr:
		if x.Name() == "main" {
			r.found = true
		}
	case *ast.BlockStmt:
		// ast.Walk доходит до BlockStmt как до Expr и в его инструкции
		// не спускается — обходим их сами.
		for _, s := range x.Stmts() {
			if err := ast.Walk(r, s); err != nil {
				return err
			}
		}
	}
	return nil
}
