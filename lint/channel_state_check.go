package main

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"slices"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

var ChannelStateCheck = withTypedFiles(loadTypedPackages, func(opts ...cop.FuncOption) *prog.Func {
	return prog.FromFile(newChannelStateCheck(opts...))
}, false)

func newChannelStateCheck(opts ...cop.FuncOption) *cop.Func {
	return cop.New(cop.Meta{
		Name:        "Lint/ChannelStateCheck",
		Description: "Channel length and capacity checks do not reserve a value or capacity",
		Severity:    cop.Warning,
	}, checkChannelStateCheck, append([]cop.FuncOption{cop.WithTypes(), cop.WithScope(outsideFrozenConfig)}, opts...)...)
}

type stateCheckGuard struct {
	channel ast.Expr
	send    bool
}

type stateCheckVisitor struct {
	pass        *cop.Pass
	nonblocking map[ast.Node]bool
}

func checkChannelStateCheck(p *cop.Pass) {
	if p.Info == nil || ast.IsGenerated(p.File) {
		return
	}
	v := stateCheckVisitor{pass: p, nonblocking: make(map[ast.Node]bool)}
	v.walk(p.File, nil)
}

func (v *stateCheckVisitor) walk(node ast.Node, guards []stateCheckGuard) {
	if node == nil {
		return
	}
	ast.Inspect(node, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.FuncLit:
			// An outer check says nothing about when a closure will run.
			v.walk(node.Body, nil)
			return false
		case *ast.BlockStmt:
			v.statements(node.List, guards)
			return false
		case *ast.CaseClause:
			for _, expr := range node.List {
				v.walk(expr, guards)
			}
			v.statements(node.Body, guards)
			return false
		case *ast.CommClause:
			v.walk(node.Comm, guards)
			v.statements(node.Body, v.afterWrites(node.Comm, guards))
			return false
		case *ast.BinaryExpr:
			v.walk(node.X, guards)
			rightGuards := v.afterWrites(node.X, guards)
			if node.Op == token.LAND || node.Op == token.LOR {
				rightGuards = slices.Concat(rightGuards, v.conditions(node.X, node.Op == token.LAND))
			}
			v.walk(node.Y, rightGuards)
			return false
		case *ast.IfStmt:
			v.walk(node.Init, guards)
			guards = v.afterWrites(node.Init, guards)
			v.walk(node.Cond, guards)
			v.walk(node.Body, slices.Concat(guards, v.conditions(node.Cond, true)))
			v.walk(node.Else, slices.Concat(guards, v.conditions(node.Cond, false)))
			return false
		case *ast.ForStmt, *ast.RangeStmt:
			// A loop's writes can invalidate an outer check on later iterations.
			loopGuards := v.afterWrites(node, guards)
			switch node := node.(type) {
			case *ast.ForStmt:
				v.walk(node.Init, loopGuards)
				v.walk(node.Cond, loopGuards)
				bodyGuards := slices.Concat(loopGuards, v.conditions(node.Cond, true))
				v.walk(node.Body, bodyGuards)
				v.walk(node.Post, v.afterWrites(node.Body, bodyGuards))
			case *ast.RangeStmt:
				v.walk(node.X, guards)
				v.walk(node.Key, loopGuards)
				v.walk(node.Value, loopGuards)
				v.walk(node.Body, loopGuards)
			}
			return false
		case *ast.SwitchStmt:
			v.walk(node.Init, guards)
			guards = v.afterWrites(node.Init, guards)
			v.walk(node.Tag, guards)
			if node.Tag != nil {
				v.walk(node.Body, guards)
				return false
			}
			var falling []stateCheckGuard
			falls := false
			for _, stmt := range node.Body.List {
				clause := stmt.(*ast.CaseClause)
				for _, expr := range clause.List {
					v.walk(expr, guards)
				}
				bodyGuards := guards
				if len(clause.List) == 1 {
					bodyGuards = slices.Concat(guards, v.conditions(clause.List[0], true))
				}
				if falls {
					if clause.List == nil {
						bodyGuards = falling
					} else {
						bodyGuards = v.commonGuards(bodyGuards, falling)
					}
				}
				v.statements(clause.Body, bodyGuards)
				falls = false
				if len(clause.Body) > 0 {
					branch, ok := clause.Body[len(clause.Body)-1].(*ast.BranchStmt)
					falls = ok && branch.Tok == token.FALLTHROUGH
				}
				falling = v.afterWrites(clause, bodyGuards)
			}
			return false
		case *ast.TypeSwitchStmt:
			v.walk(node.Init, guards)
			guards = v.afterWrites(node.Init, guards)
			v.walk(node.Assign, guards)
			v.walk(node.Body, v.afterWrites(node.Assign, guards))
			return false
		case *ast.SelectStmt:
			nonblocking := slices.ContainsFunc(node.Body.List, func(stmt ast.Stmt) bool {
				return stmt.(*ast.CommClause).Comm == nil
			})
			if nonblocking {
				for _, stmt := range node.Body.List {
					switch comm := stmt.(*ast.CommClause).Comm.(type) {
					case *ast.SendStmt:
						v.nonblocking[comm] = true
					case *ast.ExprStmt:
						v.nonblocking[ast.Unparen(comm.X)] = true
					case *ast.AssignStmt:
						for _, rhs := range comm.Rhs {
							v.nonblocking[ast.Unparen(rhs)] = true
						}
					}
				}
			}
		case *ast.SendStmt:
			v.operation(node, node.Chan, true, guards)
		case *ast.UnaryExpr:
			if node.Op == token.ARROW {
				v.operation(node, node.X, false, guards)
			}
		}
		return true
	})
}

func (v *stateCheckVisitor) statements(stmts []ast.Stmt, guards []stateCheckGuard) {
	for _, stmt := range stmts {
		if _, labelled := stmt.(*ast.LabeledStmt); labelled {
			guards = nil
		}
		v.walk(stmt, guards)
		guards = v.afterWrites(stmt, guards)
		if branch, ok := stmt.(*ast.BranchStmt); ok && branch.Tok == token.GOTO {
			return
		}
		if stmt, ok := stmt.(*ast.IfStmt); ok && stmt.Else == nil && stateCheckExits(stmt.Body) {
			guards = slices.Concat(guards, v.conditions(stmt.Cond, false))
		}
	}
}

func (v *stateCheckVisitor) commonGuards(a, b []stateCheckGuard) []stateCheckGuard {
	var common []stateCheckGuard
	for _, x := range a {
		if slices.ContainsFunc(b, func(y stateCheckGuard) bool { return x.send == y.send && v.sameChannel(x.channel, y.channel) }) {
			common = append(common, x)
		}
	}
	return common
}

func stateCheckExits(block *ast.BlockStmt) bool {
	if len(block.List) == 0 {
		return false
	}
	switch stmt := block.List[len(block.List)-1].(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BranchStmt:
		return stmt.Tok == token.CONTINUE || stmt.Tok == token.BREAK
	}
	return false
}

func (v *stateCheckVisitor) operation(node ast.Node, channel ast.Expr, send bool, guards []stateCheckGuard) {
	if v.nonblocking[node] {
		return
	}
	for _, guard := range guards {
		if guard.send == send && v.sameChannel(guard.channel, channel) {
			v.pass.Report(node, "channel state check does not reserve capacity or a value; use select or explicit synchronization instead of guarding a potentially blocking channel operation with len/cap")
			return
		}
	}
}

func (v *stateCheckVisitor) conditions(expr ast.Expr, truth bool) []stateCheckGuard {
	if expr == nil {
		return nil
	}
	switch expr := ast.Unparen(expr).(type) {
	case *ast.UnaryExpr:
		if expr.Op == token.NOT {
			return v.conditions(expr.X, !truth)
		}
	case *ast.BinaryExpr:
		if (expr.Op == token.LAND && truth) || (expr.Op == token.LOR && !truth) {
			return slices.Concat(v.conditions(expr.X, truth), v.conditions(expr.Y, truth))
		}
		op, left, right := expr.Op, expr.X, expr.Y
		channel := v.builtinChannel(left, "len")
		if channel == nil {
			channel = v.builtinChannel(right, "len")
			right = left
			op = stateCheckReverse(op)
		}
		if channel == nil {
			return nil
		}
		if !truth {
			op = stateCheckNegate(op)
		}
		if capacity := v.builtinChannel(right, "cap"); capacity != nil && v.sameChannel(channel, capacity) {
			if op == token.LSS || op == token.NEQ {
				return []stateCheckGuard{{channel: channel, send: true}}
			}
			return nil
		}
		value := v.pass.Info.Types[ast.Unparen(right)].Value
		if value == nil || value.Kind() != constant.Int {
			return nil
		}
		zero, one := constant.MakeInt64(0), constant.MakeInt64(1)
		if (op == token.GTR && constant.Compare(value, token.GEQ, zero)) ||
			(op == token.GEQ && constant.Compare(value, token.GEQ, one)) ||
			(op == token.EQL && constant.Compare(value, token.GEQ, one)) ||
			(op == token.NEQ && constant.Sign(value) == 0) {
			return []stateCheckGuard{{channel: channel}}
		}
	}
	return nil
}

func stateCheckReverse(op token.Token) token.Token {
	switch op {
	case token.LSS:
		return token.GTR
	case token.LEQ:
		return token.GEQ
	case token.GTR:
		return token.LSS
	case token.GEQ:
		return token.LEQ
	}
	return op
}

func stateCheckNegate(op token.Token) token.Token {
	switch op {
	case token.LSS:
		return token.GEQ
	case token.LEQ:
		return token.GTR
	case token.GTR:
		return token.LEQ
	case token.GEQ:
		return token.LSS
	case token.EQL:
		return token.NEQ
	case token.NEQ:
		return token.EQL
	}
	return token.ILLEGAL
}

func (v *stateCheckVisitor) builtinChannel(expr ast.Expr, name string) ast.Expr {
	call, ok := ast.Unparen(expr).(*ast.CallExpr)
	if !ok || len(call.Args) != 1 || v.pass.CalleeObject(call) != types.Universe.Lookup(name) {
		return nil
	}
	channel := ast.Unparen(call.Args[0])
	typ := v.pass.Info.TypeOf(channel)
	if typ == nil {
		return nil
	}
	if _, ok := typ.Underlying().(*types.Chan); !ok || !v.sameChannel(channel, channel) {
		return nil
	}
	return channel
}

// Match resolved storage expressions, not spelling or repeated calls.
func (v *stateCheckVisitor) sameChannel(a, b ast.Expr) bool {
	a, b = ast.Unparen(a), ast.Unparen(b)
	switch a := a.(type) {
	case *ast.Ident:
		b, ok := b.(*ast.Ident)
		return ok && v.pass.Info.ObjectOf(a) != nil && v.pass.Info.ObjectOf(a) == v.pass.Info.ObjectOf(b)
	case *ast.SelectorExpr:
		b, ok := b.(*ast.SelectorExpr)
		return ok && v.pass.Info.ObjectOf(a.Sel) != nil && v.pass.Info.ObjectOf(a.Sel) == v.pass.Info.ObjectOf(b.Sel) && v.sameChannel(a.X, b.X)
	case *ast.StarExpr:
		b, ok := b.(*ast.StarExpr)
		return ok && v.sameChannel(a.X, b.X)
	}
	return false
}

func (v *stateCheckVisitor) afterWrites(node ast.Node, guards []stateCheckGuard) []stateCheckGuard {
	if node == nil || len(guards) == 0 {
		return guards
	}
	var writes []ast.Expr
	ast.Inspect(node, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.AssignStmt:
			writes = append(writes, node.Lhs...)
		case *ast.IncDecStmt:
			writes = append(writes, node.X)
		case *ast.RangeStmt:
			if node.Tok == token.ASSIGN {
				if node.Key != nil {
					writes = append(writes, node.Key)
				}
				if node.Value != nil {
					writes = append(writes, node.Value)
				}
			}
		}
		return true
	})
	if len(writes) == 0 {
		return guards
	}
	var retained []stateCheckGuard
	for _, guard := range guards {
		changed := slices.ContainsFunc(writes, func(write ast.Expr) bool {
			found := false
			ast.Inspect(guard.channel, func(node ast.Node) bool {
				if expr, ok := node.(ast.Expr); ok && v.sameChannel(expr, write) {
					found = true
				}
				return !found
			})
			return found
		})
		if !changed {
			retained = append(retained, guard)
		}
	}
	return retained
}
