package main

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"maps"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

// CancellationSafeResultSend checks straight-line setup of local unbuffered
// result channels with one inline producer and one returning context select.
// Escapes, extra receivers, and buffered channels are outside this analysis.
var CancellationSafeResultSend = prog.FromFile(cop.New(cop.Meta{
	Name:        "Lint/CancellationSafeResultSend",
	Description: "Do not strand result senders when callers return on cancellation",
	Severity:    cop.Warning,
}, checkCancellationSafeResultSend, cop.WithTypes(), cop.WithScope(outsideFrozenConfig)))

func checkCancellationSafeResultSend(p *cop.Pass) {
	if p.Info == nil || p.IsTestFile() || ast.IsGenerated(p.File) {
		return
	}
	ast.Inspect(p.File, func(n ast.Node) bool {
		switch fn := n.(type) {
		case *ast.FuncDecl:
			checkResultSendBody(p, fn.Body)
		case *ast.FuncLit:
			checkResultSendBody(p, fn.Body)
		}
		return true
	})
}

type resultChannel struct {
	object types.Object
	pos    token.Pos
}

func checkResultSendBody(p *cop.Pass, body *ast.BlockStmt) {
	if body == nil {
		return
	}
	aliases := make(map[types.Object]types.Object)
	allowed := make(map[*ast.Ident]bool)
	var channels []resultChannel
	var workers []*ast.GoStmt
	var selects []*ast.SelectStmt
	bind := func(id *ast.Ident, rhs ast.Expr) {
		obj := p.Info.Defs[id]
		if obj == nil {
			return
		}
		if call, ok := ast.Unparen(rhs).(*ast.CallExpr); ok && p.CalleeObject(call) == types.Universe.Lookup("make") && len(call.Args) >= 1 {
			if typ := p.Info.TypeOf(call); typ != nil {
				if _, ok := typ.Underlying().(*types.Chan); ok && (len(call.Args) == 1 || len(call.Args) == 2 && resultZero(p, call.Args[1])) {
					aliases[obj] = obj
					channels = append(channels, resultChannel{obj, id.Pos()})
				}
			}
		} else if source := streamObject(p, rhs); source != nil {
			if original := aliases[source]; original != nil {
				source = original
			}
			aliases[obj] = source
			allowed[ast.Unparen(rhs).(*ast.Ident)] = true
		}
	}
	for _, stmt := range body.List {
		switch stmt := stmt.(type) {
		case *ast.AssignStmt:
			if len(stmt.Lhs) == len(stmt.Rhs) && stmt.Tok == token.DEFINE {
				for i, lhs := range stmt.Lhs {
					if id, ok := lhs.(*ast.Ident); ok {
						bind(id, stmt.Rhs[i])
					}
				}
			}
		case *ast.DeclStmt:
			if decl, ok := stmt.Decl.(*ast.GenDecl); ok && decl.Tok == token.VAR {
				for _, spec := range decl.Specs {
					value := spec.(*ast.ValueSpec)
					if len(value.Names) == len(value.Values) {
						for i, id := range value.Names {
							bind(id, value.Values[i])
						}
					}
				}
			}
		case *ast.GoStmt:
			if fn, ok := stmt.Call.Fun.(*ast.FuncLit); ok {
				workers = append(workers, stmt)
				var params []*ast.Ident
				for _, field := range fn.Type.Params.List {
					params = append(params, field.Names...)
				}
				if len(params) == len(stmt.Call.Args) {
					for i, arg := range stmt.Call.Args {
						if source := streamObject(p, arg); source != nil {
							if original := aliases[source]; original != nil {
								source = original
							}
							aliases[p.Info.Defs[params[i]]] = source
							allowed[ast.Unparen(arg).(*ast.Ident)] = true
						}
					}
				}
			}
		case *ast.SelectStmt:
			selects = append(selects, stmt)
		}
	}
	if len(channels) == 0 || len(workers) == 0 || len(selects) == 0 {
		return
	}
	parents := make(map[ast.Node]ast.Node)
	var stack []ast.Node
	ast.Inspect(body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		if len(stack) > 0 {
			parents[n] = stack[len(stack)-1]
		}
		stack = append(stack, n)
		return true
	})
	contexts := resultContextUses(p, body)
	for _, ch := range channels {
		for _, sel := range selects {
			cancellations, known := resultCancellations(p, sel, true, contexts)
			if !known || len(cancellations) == 0 || sel.Pos() < ch.pos {
				continue
			}
			checkResultChannel(p, body, ch, sel, cancellations, contexts, workers, aliases, allowed, parents)
		}
	}
}

func resultZero(p *cop.Pass, expr ast.Expr) bool {
	v := p.Info.Types[expr].Value
	return v != nil && v.Kind() == constant.Int && constant.Sign(v) == 0
}

type resultContext struct {
	object types.Object
}

func resultCancellations(p *cop.Pass, sel *ast.SelectStmt, returning bool, contexts map[*ast.Ident]resultContext) (map[resultContext]bool, bool) {
	found := make(map[resultContext]bool)
	for _, stmt := range sel.Body.List {
		clause := stmt.(*ast.CommClause)
		var recv *ast.UnaryExpr
		switch comm := clause.Comm.(type) {
		case *ast.ExprStmt:
			recv, _ = ast.Unparen(comm.X).(*ast.UnaryExpr)
		case *ast.AssignStmt:
			if len(comm.Rhs) == 1 {
				recv, _ = ast.Unparen(comm.Rhs[0]).(*ast.UnaryExpr)
			}
		}
		if recv == nil || recv.Op != token.ARROW {
			continue
		}
		call, ok := ast.Unparen(recv.X).(*ast.CallExpr)
		if !ok || len(call.Args) != 0 {
			continue
		}
		method, ok := p.CalleeObject(call).(*types.Func)
		if !ok || method.Pkg() == nil || method.Pkg().Path() != "context" || method.Name() != "Done" {
			continue
		}
		expr, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
		if !ok {
			continue
		}
		if returning {
			if len(clause.Body) != 1 {
				continue
			}
			if _, ok := clause.Body[0].(*ast.ReturnStmt); !ok {
				continue
			}
		}
		id, ok := ast.Unparen(expr.X).(*ast.Ident)
		if !ok || contexts[id].object == nil {
			return nil, false
		}
		found[contexts[id]] = true
	}
	return found, true
}

func checkResultChannel(p *cop.Pass, body *ast.BlockStmt, ch resultChannel, consumer *ast.SelectStmt, cancellations map[resultContext]bool, contexts map[*ast.Ident]resultContext, workers []*ast.GoStmt, aliases map[types.Object]types.Object, allowed map[*ast.Ident]bool, parents map[ast.Node]ast.Node) {
	uses := make(map[*ast.Ident]bool)
	var sends []*ast.SendStmt
	var producer *ast.FuncLit
	receives, uncertain := 0, false
	mark := func(expr ast.Expr) bool {
		id, ok := ast.Unparen(expr).(*ast.Ident)
		if !ok || aliases[p.Info.ObjectOf(id)] != ch.object {
			return false
		}
		uses[id] = true
		return true
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SendStmt:
			if !mark(n.Chan) {
				break
			}
			var fn *ast.FuncLit
			for parent := parents[n]; parent != nil; parent = parents[parent] {
				if fn, _ = parent.(*ast.FuncLit); fn != nil {
					break
				}
			}
			inline := false
			for _, worker := range workers {
				if worker.Call.Fun == fn && worker.Pos() > ch.pos && worker.Pos() < consumer.Pos() {
					inline = true
					break
				}
			}
			if !inline || producer != nil && producer != fn {
				uncertain = true
			}
			producer = fn
			if clause, ok := parents[n].(*ast.CommClause); ok && clause.Comm == n {
				if sel, ok := parents[parents[clause]].(*ast.SelectStmt); ok {
					exits, known := resultCancellations(p, sel, false, contexts)
					if !known {
						uncertain = true
					}
					protected := known
					for ctx := range cancellations {
						protected = protected && exits[ctx]
					}
					if protected {
						break
					}
					for _, stmt := range sel.Body.List {
						if stmt.(*ast.CommClause).Comm == nil {
							return true
						}
					}
				}
			}
			sends = append(sends, n)
		case *ast.UnaryExpr:
			if n.Op != token.ARROW || !mark(n.X) {
				break
			}
			receives++
			comm := parents[n]
			if _, ok := comm.(*ast.ParenExpr); ok {
				comm = parents[comm]
			}
			clause, ok := parents[comm].(*ast.CommClause)
			if !ok || clause.Comm != comm || parents[parents[clause]] != consumer {
				uncertain = true
			}
		case *ast.CallExpr:
			if p.CalleeObject(n) == types.Universe.Lookup("close") && len(n.Args) == 1 {
				mark(n.Args[0])
			}
		}
		return true
	})
	for id := range cop.Nodes[*ast.Ident](body) {
		if aliases[p.Info.Uses[id]] == ch.object && !uses[id] && !allowed[id] {
			uncertain = true
		}
	}
	if !uncertain && receives == 1 {
		for _, send := range sends {
			p.Report(send, "result send can block after the caller returns on context cancellation; select on cancellation or use a buffered one-shot result channel")
		}
	}
}

// Snapshot straight-line assignments; goroutine arguments copy values, captures do not.
func resultContextUses(p *cop.Pass, body *ast.BlockStmt) map[*ast.Ident]resultContext {
	uses := make(map[*ast.Ident]resultContext)
	type environment map[types.Object]resultContext
	var value func(environment, ast.Expr) resultContext
	value = func(env environment, expr ast.Expr) resultContext {
		if expr == nil {
			return resultContext{}
		}
		if call, ok := ast.Unparen(expr).(*ast.CallExpr); ok {
			if len(call.Args) == 1 && p.Info.Types[call.Fun].IsType() && types.Identical(p.Info.TypeOf(call), p.Info.TypeOf(call.Args[0])) {
				return value(env, call.Args[0])
			}
			if fn := p.CalleeObject(call); fn != nil && fn.Pkg() != nil && fn.Pkg().Path() == "context" && fn.Name() == "WithValue" && len(call.Args) == 3 {
				return value(env, call.Args[0])
			}
		}
		obj := streamObject(p, expr)
		if obj == nil {
			return resultContext{}
		}
		if v, ok := env[obj]; ok {
			return v
		}
		return resultContext{object: obj}
	}
	record := func(node ast.Node, env environment) {
		ast.Inspect(node, func(n ast.Node) bool {
			if _, ok := n.(*ast.FuncLit); ok {
				return false
			}
			if id, ok := n.(*ast.Ident); ok && p.Info.Uses[id] != nil {
				uses[id] = value(env, id)
			}
			return true
		})
	}
	written := func(node ast.Node) map[types.Object]bool {
		objects := make(map[types.Object]bool)
		mark := func(expr ast.Expr) {
			if obj := streamObject(p, expr); obj != nil {
				objects[obj] = true
			}
		}
		ast.Inspect(node, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.AssignStmt:
				for _, lhs := range n.Lhs {
					mark(lhs)
				}
			case *ast.RangeStmt:
				if n.Key != nil {
					mark(n.Key)
				}
				if n.Value != nil {
					mark(n.Value)
				}
			case *ast.UnaryExpr:
				if n.Op == token.AND {
					mark(n.X)
				}
			}
			return true
		})
		return objects
	}
	var scan func(*ast.BlockStmt, environment)
	scan = func(block *ast.BlockStmt, env environment) {
		escaped := make(map[types.Object]bool)
		for i, stmt := range block.List {
			ast.Inspect(stmt, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					if fn, ok := ast.Unparen(call.Fun).(*ast.FuncLit); ok {
						for obj := range written(fn.Body) {
							env[obj] = resultContext{}
						}
					}
				}
				if _, ok := n.(*ast.FuncLit); ok {
					return false
				}
				if address, ok := n.(*ast.UnaryExpr); ok && address.Op == token.AND {
					if obj := streamObject(p, address.X); obj != nil {
						escaped[obj] = true
					}
				}
				return true
			})
			for obj := range escaped {
				env[obj] = resultContext{}
			}
			switch stmt := stmt.(type) {
			case *ast.AssignStmt:
				record(stmt, env)
				values := make([]resultContext, len(stmt.Lhs))
				for j := range stmt.Lhs {
					if len(stmt.Lhs) == len(stmt.Rhs) {
						values[j] = value(env, stmt.Rhs[j])
					}
				}
				for j, lhs := range stmt.Lhs {
					if obj := streamObject(p, lhs); obj != nil {
						env[obj] = values[j]
					}
				}
			case *ast.DeclStmt:
				record(stmt, env)
				if decl, ok := stmt.Decl.(*ast.GenDecl); ok && decl.Tok == token.VAR {
					for _, spec := range decl.Specs {
						v := spec.(*ast.ValueSpec)
						for j, id := range v.Names {
							ctx := resultContext{}
							if len(v.Values) == len(v.Names) {
								ctx = value(env, v.Values[j])
							}
							env[p.Info.Defs[id]] = ctx
						}
					}
				}
			case *ast.GoStmt:
				fn, ok := ast.Unparen(stmt.Call.Fun).(*ast.FuncLit)
				if !ok {
					continue
				}
				child := maps.Clone(env)
				for _, next := range block.List[i+1:] {
					for obj := range written(next) {
						child[obj] = resultContext{}
					}
				}
				var params []*ast.Ident
				for _, field := range fn.Type.Params.List {
					params = append(params, field.Names...)
				}
				if len(params) == len(stmt.Call.Args) {
					for j, arg := range stmt.Call.Args {
						child[p.Info.Defs[params[j]]] = value(env, arg)
					}
				}
				scan(fn.Body, child)
			default:
				// Branch/loop writes and address escapes cannot be resolved locally.
				for obj := range written(stmt) {
					env[obj] = resultContext{}
				}
				record(stmt, env)
			}
			for obj := range escaped {
				env[obj] = resultContext{}
			}
		}
	}
	scan(body, make(environment))
	return uses
}
