package main

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
	"golang.org/x/tools/go/cfg"
)

var CancellationLoop = withTypedFiles(loadTypedPackages, func(opts ...cop.FuncOption) *prog.Func {
	return prog.FromFile(newCancellationLoop(opts...))
}, false)

var ClosedChannelLoop = withTypedFiles(loadTypedPackages, func(opts ...cop.FuncOption) *prog.Func {
	return prog.FromFile(newClosedChannelLoop(opts...))
}, false)

func newCancellationLoop(opts ...cop.FuncOption) *cop.Func {
	return cop.New(cop.Meta{
		Name: "Lint/CancellationLoop", Description: "leave polling loops after context cancellation",
		Severity: cop.Warning,
	}, func(p *cop.Pass) { checkChannelLoops(p, true) }, append([]cop.FuncOption{cop.WithTypes(), cop.WithScope(outsideFrozenConfig)}, opts...)...)
}

func newClosedChannelLoop(opts ...cop.FuncOption) *cop.Func {
	return cop.New(cop.Meta{
		Name: "Lint/ClosedChannelLoop", Description: "stop polling a channel after observing closure",
		Severity: cop.Warning,
	}, func(p *cop.Pass) { checkChannelLoops(p, false) }, append([]cop.FuncOption{cop.WithTypes(), cop.WithScope(outsideFrozenConfig)}, opts...)...)
}

type channelLoopCandidate struct {
	comm   ast.Node
	clause *ast.CommClause
	ch     ast.Expr
	ok     types.Object
}

func checkChannelLoops(p *cop.Pass, cancellation bool) {
	if p.Info == nil || ast.IsGenerated(p.File) {
		return
	}
	ast.Inspect(p.File, func(n ast.Node) bool {
		var body *ast.BlockStmt
		switch n := n.(type) {
		case *ast.FuncDecl:
			body = n.Body
		case *ast.FuncLit:
			body = n.Body
		default:
			return true
		}
		if body == nil {
			return true
		}
		candidates := channelLoopCandidates(p, body, cancellation)
		if len(candidates) == 0 {
			return true
		}
		graph := cfg.New(body, func(call *ast.CallExpr) bool { return p.CalleeObject(call) != types.Universe.Lookup("panic") })
		flow := newChannelLoopFlow(graph, body)
		for _, candidate := range candidates {
			if !channelLoopRepeats(p, flow, candidate) {
				continue
			}
			if cancellation {
				p.Report(candidate.comm, "cancellation can return to the same select; return or break the enclosing loop, not just the select")
			} else {
				p.Report(candidate.comm, "closed channel can be polled again; leave the loop or disable the channel after !ok")
			}
		}
		return true
	})
}

// Restrict warnings to unconditional loops; conditional loops may exit through state updates.
func channelLoopCandidates(p *cop.Pass, body *ast.BlockStmt, cancellation bool) []channelLoopCandidate {
	var candidates []channelLoopCandidate
	var stack []ast.Node
	ast.Inspect(body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		if _, nested := n.(*ast.FuncLit); nested {
			return false
		}
		stack = append(stack, n)
		var recv *ast.UnaryExpr
		var result types.Object
		switch n := n.(type) {
		case *ast.ExprStmt:
			recv, _ = ast.Unparen(n.X).(*ast.UnaryExpr)
		case *ast.AssignStmt:
			if len(n.Rhs) == 1 && (len(n.Lhs) == 2 || cancellation && len(n.Lhs) == 1) {
				recv, _ = ast.Unparen(n.Rhs[0]).(*ast.UnaryExpr)
				if len(n.Lhs) == 2 {
					if id, ok := n.Lhs[1].(*ast.Ident); ok && id.Name != "_" {
						result = p.Info.ObjectOf(id)
					}
				}
			}
		case *ast.ValueSpec:
			if len(n.Values) == 1 && len(n.Names) == 2 {
				recv, _ = ast.Unparen(n.Values[0]).(*ast.UnaryExpr)
				if n.Names[1].Name != "_" {
					result = p.Info.ObjectOf(n.Names[1])
				}
			}
		}
		if recv == nil || recv.Op != token.ARROW || channelType(p.Info.TypeOf(recv.X)) == nil {
			return true
		}
		var clause *ast.CommClause
		inLoop := false
		for i := len(stack) - 2; i >= 0; i-- {
			switch ancestor := stack[i].(type) {
			case *ast.CommClause:
				if ancestor.Comm == n {
					clause = ancestor
				}
			case *ast.ForStmt:
				inLoop = ancestor.Cond == nil
				goto foundLoop
			case *ast.RangeStmt:
				goto foundLoop
			}
		}
	foundLoop:
		if !inLoop {
			return true
		}
		if cancellation {
			call, ok := ast.Unparen(recv.X).(*ast.CallExpr)
			if !ok || clause == nil {
				return true
			}
			obj := p.CalleeObject(call)
			if obj == nil || obj.Pkg() == nil || obj.Pkg().Path() != "context" || obj.Name() != "Done" {
				return true
			}
		} else if result == nil || !channelLoopSame(p, recv.X, recv.X) {
			return true
		}
		if cancellation {
			call := ast.Unparen(recv.X).(*ast.CallExpr)
			sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
			if !ok || !channelLoopSame(p, sel.X, sel.X) {
				return true
			}
			result = nil
		}
		candidates = append(candidates, channelLoopCandidate{comm: n, clause: clause, ch: recv.X, ok: result})
		return true
	})
	return candidates
}

type channelLoopPoint struct {
	block  *cfg.Block
	index  int
	closed bool
}

type channelLoopFlow struct {
	graph        *cfg.CFG
	conditions   map[ast.Expr]ast.Expr
	selectWrites map[ast.Node]bool
}

func newChannelLoopFlow(graph *cfg.CFG, body *ast.BlockStmt) channelLoopFlow {
	flow := channelLoopFlow{graph: graph, conditions: make(map[ast.Expr]ast.Expr), selectWrites: make(map[ast.Node]bool)}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.IfStmt:
			flow.conditions[n.Cond] = nil
		case *ast.SwitchStmt:
			for _, clause := range n.Body.List {
				for _, expr := range clause.(*ast.CaseClause).List {
					flow.conditions[expr] = n.Tag
				}
			}
		case *ast.CommClause:
			if assign, ok := n.Comm.(*ast.AssignStmt); ok {
				flow.selectWrites[assign] = true
			}
		}
		return true
	})
	return flow
}

func channelLoopRepeats(p *cop.Pass, flow channelLoopFlow, candidate channelLoopCandidate) bool {
	var start channelLoopPoint
	for _, block := range flow.graph.Blocks {
		if candidate.clause != nil && block.Kind == cfg.KindSelectCaseBody && block.Stmt == candidate.clause {
			start = channelLoopPoint{block: block}
			break
		}
		if candidate.clause == nil {
			for i, node := range block.Nodes {
				if node == candidate.comm {
					start = channelLoopPoint{block: block, index: i + 1}
				}
			}
		}
	}
	if start.block == nil || !start.block.Live {
		return false
	}
	start.closed = candidate.ok == nil
	queue := []channelLoopPoint{start}
	seen := make(map[channelLoopPoint]bool)
	for len(queue) > 0 {
		point := queue[0]
		queue = queue[1:]
		if seen[point] {
			continue
		}
		seen[point] = true
		if clause, ok := point.block.Stmt.(*ast.CommClause); ok && point.block.Kind == cfg.KindSelectCaseBody && clause != candidate.clause {
			if channelLoopMutation(p, clause.Comm, candidate) {
				continue
			}
		}
		blocked := false
		for _, node := range point.block.Nodes[point.index:] {
			if node == candidate.comm {
				if point.closed {
					return true
				}
				blocked = true
				break
			}
			if !flow.selectWrites[node] && channelLoopMutation(p, node, candidate) {
				blocked = true
				break
			}
		}
		if blocked {
			continue
		}
		for edge, next := range point.block.Succs {
			if candidate.ok != nil && len(point.block.Succs) == 2 && len(point.block.Nodes) > 0 {
				expr, _ := point.block.Nodes[len(point.block.Nodes)-1].(ast.Expr)
				if value, known := flow.condition(p, expr, candidate.ok); known {
					point.closed = true
					if value != (edge == 0) {
						continue
					}
				}
			}
			queue = append(queue, channelLoopPoint{block: next, closed: point.closed})
		}
	}
	return false
}

func (flow channelLoopFlow) condition(p *cop.Pass, expr ast.Expr, result types.Object) (bool, bool) {
	tag, found := flow.conditions[expr]
	if !found {
		return false, false
	}
	usesResult := false
	for _, root := range []ast.Expr{expr, tag} {
		if root == nil {
			continue
		}
		ast.Inspect(root, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && p.Info.ObjectOf(id) == result {
				usesResult = true
			}
			return true
		})
	}
	if !usesResult {
		return false, false
	}
	value, known := channelLoopBool(p, expr, result)
	if tag == nil {
		return value, known
	}
	want, tagKnown := channelLoopBool(p, tag, result)
	return value == want, known && tagKnown
}

func channelLoopMutation(p *cop.Pass, node ast.Node, candidate channelLoopCandidate) bool {
	var lhsList []ast.Expr
	switch node := node.(type) {
	case *ast.AssignStmt:
		lhsList = node.Lhs
	case *ast.ValueSpec:
		for _, name := range node.Names {
			lhsList = append(lhsList, name)
		}
	default:
		return false
	}
	for _, lhs := range lhsList {
		if id, ok := lhs.(*ast.Ident); ok && candidate.ok != nil && p.Info.ObjectOf(id) == candidate.ok {
			return true
		}
		expr := ast.Unparen(candidate.ch)
		if call, ok := expr.(*ast.CallExpr); ok {
			if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok {
				expr = sel.X
			}
		}
		for {
			if channelLoopSame(p, lhs, expr) {
				return true
			}
			sel, ok := ast.Unparen(expr).(*ast.SelectorExpr)
			if !ok {
				break
			}
			expr = sel.X
		}
	}
	return false
}

func channelLoopSame(p *cop.Pass, x, y ast.Expr) bool {
	switch x := ast.Unparen(x).(type) {
	case *ast.Ident:
		y, ok := ast.Unparen(y).(*ast.Ident)
		return ok && p.Info.ObjectOf(x) != nil && p.Info.ObjectOf(x) == p.Info.ObjectOf(y)
	case *ast.SelectorExpr:
		y, ok := ast.Unparen(y).(*ast.SelectorExpr)
		return ok && p.Info.ObjectOf(x.Sel) != nil && p.Info.ObjectOf(x.Sel) == p.Info.ObjectOf(y.Sel) && channelLoopSame(p, x.X, y.X)
	}
	return false
}

// Evaluate simple closure tests with the comma-ok result known to be false.
func channelLoopBool(p *cop.Pass, expr ast.Expr, result types.Object) (bool, bool) {
	if expr == nil {
		return false, false
	}
	switch expr := ast.Unparen(expr).(type) {
	case *ast.Ident:
		if p.Info.ObjectOf(expr) == types.Universe.Lookup("true") {
			return true, true
		}
		if p.Info.ObjectOf(expr) == types.Universe.Lookup("false") {
			return false, true
		}
		if p.Info.ObjectOf(expr) == result {
			return false, true
		}
	case *ast.UnaryExpr:
		if expr.Op == token.NOT {
			value, known := channelLoopBool(p, expr.X, result)
			return !value, known
		}
	case *ast.BinaryExpr:
		if expr.Op == token.EQL || expr.Op == token.NEQ {
			for _, pair := range [][2]ast.Expr{{expr.X, expr.Y}, {expr.Y, expr.X}} {
				id, ok := ast.Unparen(pair[1]).(*ast.Ident)
				if !ok || (p.Info.ObjectOf(id) != types.Universe.Lookup("false") && p.Info.ObjectOf(id) != types.Universe.Lookup("true")) {
					continue
				}
				if value, known := channelLoopBool(p, pair[0], result); known {
					equal := value == (id.Name == "true")
					return equal == (expr.Op == token.EQL), true
				}
			}
		}
	}
	return false, false
}
