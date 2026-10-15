package main

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

// NonblockingChannelReplacement checks adjacent drain/resend fallbacks, not
// arbitrary queue protocols or channel expressions with calls or indexes.
var NonblockingChannelReplacement = prog.FromFile(cop.New(cop.Meta{
	Name:        "Lint/NonblockingChannelReplacement",
	Description: "Keep channel replacement fallbacks nonblocking",
	Severity:    cop.Warning,
}, checkNonblockingChannelReplacement, cop.WithTypes(), cop.WithScope(outsideFrozenConfig)))

func checkNonblockingChannelReplacement(p *cop.Pass) {
	if p.Info == nil || p.IsTestFile() || ast.IsGenerated(p.File) {
		return
	}
	for sel := range cop.Nodes[*ast.SelectStmt](p.File) {
		if len(sel.Body.List) != 2 {
			continue
		}
		var send *ast.SendStmt
		var fallback *ast.CommClause
		for _, stmt := range sel.Body.List {
			clause := stmt.(*ast.CommClause)
			if clause.Comm == nil {
				fallback = clause
			} else {
				send, _ = clause.Comm.(*ast.SendStmt)
			}
		}
		if send == nil || fallback == nil || len(fallback.Body) != 2 {
			continue
		}
		stable := true
		ast.Inspect(send.Value, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CallExpr:
				stable = false
			case *ast.UnaryExpr:
				stable = stable && n.Op != token.ARROW
			}
			return stable
		})
		if !stable {
			continue
		}
		recv := replacementReceive(p, fallback.Body[0])
		retry, ok := fallback.Body[1].(*ast.SendStmt)
		if recv == nil || !ok || !sameReplacementChannel(p, send.Chan, recv.X) || !sameReplacementChannel(p, send.Chan, retry.Chan) {
			continue
		}
		p.Report(recv, "channel replacement can block if another receiver drains it after the select; keep the fallback drain and resend nonblocking")
	}
}

func replacementReceive(p *cop.Pass, stmt ast.Stmt) *ast.UnaryExpr {
	var expr ast.Expr
	switch stmt := stmt.(type) {
	case *ast.ExprStmt:
		expr = stmt.X
	case *ast.AssignStmt:
		for _, lhs := range stmt.Lhs {
			id, ok := lhs.(*ast.Ident)
			if !ok || id.Name != "_" && (stmt.Tok != token.DEFINE || p.Info.Defs[id] == nil) {
				return nil
			}
		}
		if len(stmt.Rhs) == 1 {
			expr = stmt.Rhs[0]
		}
	}
	if expr != nil {
		if recv, ok := ast.Unparen(expr).(*ast.UnaryExpr); ok && recv.Op == token.ARROW {
			return recv
		}
	}
	return nil
}

func sameReplacementChannel(p *cop.Pass, left, right ast.Expr) bool {
	typ := p.Info.TypeOf(left)
	if typ == nil {
		return false
	}
	if _, ok := typ.Underlying().(*types.Chan); !ok {
		return false
	}
	return sameReplacementExpr(p, left, right)
}

func sameReplacementExpr(p *cop.Pass, left, right ast.Expr) bool {
	switch left := ast.Unparen(left).(type) {
	case *ast.Ident:
		right, ok := ast.Unparen(right).(*ast.Ident)
		obj, local := p.Info.ObjectOf(left).(*types.Var)
		return ok && local && obj.Parent() != nil && obj.Pkg() != nil && obj.Parent() != obj.Pkg().Scope() && obj == p.Info.ObjectOf(right)
	case *ast.SelectorExpr:
		right, ok := ast.Unparen(right).(*ast.SelectorExpr)
		if !ok {
			return false
		}
		l, r := p.Info.Selections[left], p.Info.Selections[right]
		return l != nil && r != nil && l.Kind() == types.FieldVal && r.Kind() == types.FieldVal && l.Obj() == r.Obj() && sameReplacementExpr(p, left.X, right.X)
	}
	return false
}
