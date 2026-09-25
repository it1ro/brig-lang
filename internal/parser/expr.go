package parser

import (
	"strconv"
	"strings"

	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/internal/lexer"
)

// expr ::= lambda_expr | or_expr
func (p *parser) parseExpr() (ast.Expr, error) {
	if e, ok, err := p.tryLambda(); ok || err != nil {
		return e, err
	}
	return p.parseOr()
}

// tryLambda различает lambda_short / lambda_full / lambda_empty.
func (p *parser) tryLambda() (ast.Expr, bool, error) {
	// lambda_empty: "(" ")" "->" expr
	if p.at(lexer.LPAREN) && p.peek(1).Type == lexer.RPAREN && p.peek(2).Type == lexer.OP_ARROW {
		start := p.cur()
		p.advance() // (
		p.advance() // )
		p.advance() // ->
		body, err := p.parseExpr()
		if err != nil {
			return nil, true, err
		}
		return ast.NewLambdaEmptyExpr(body, start.Line, start.Col), true, nil
	}
	// lambda_short: LOWER_IDENT "->" expr
	if p.at(lexer.LOWER_IDENT) && p.peek(1).Type == lexer.OP_ARROW {
		start := p.cur()
		name := p.advance().Lit
		p.advance() // ->
		body, err := p.parseExpr()
		if err != nil {
			return nil, true, err
		}
		return ast.NewLambdaShortExpr(name, body, start.Line, start.Col), true, nil
	}
	// lambda_full: "fn" "(" params ")" "->" fn_body
	if p.at(lexer.KW_FN) && p.peek(1).Type == lexer.LPAREN {
		start := p.cur()
		p.advance() // fn
		params, err := p.parseParams()
		if err != nil {
			return nil, true, err
		}
		if _, err := p.expect(lexer.OP_ARROW, "'->'"); err != nil {
			return nil, true, err
		}
		body, err := p.parseFnBody()
		if err != nil {
			return nil, true, err
		}
		return ast.NewLambdaFullExpr(params, body, start.Line, start.Col), true, nil
	}
	return nil, false, nil
}

// or_expr ::= and_expr { "or" and_expr }
func (p *parser) parseOr() (ast.Expr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.at(lexer.KW_OR) {
		op := p.advance()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = ast.NewBinaryExpr("or", left, right, op.Line, op.Col)
	}
	return left, nil
}

// and_expr ::= cmp_expr { "and" cmp_expr }
func (p *parser) parseAnd() (ast.Expr, error) {
	left, err := p.parseCmp()
	if err != nil {
		return nil, err
	}
	for p.at(lexer.KW_AND) {
		op := p.advance()
		right, err := p.parseCmp()
		if err != nil {
			return nil, err
		}
		left = ast.NewBinaryExpr("and", left, right, op.Line, op.Col)
	}
	return left, nil
}

// cmp_expr ::= pipe_expr [ cmp_op pipe_expr ]  (non-assoc)
func (p *parser) parseCmp() (ast.Expr, error) {
	left, err := p.parsePipe()
	if err != nil {
		return nil, err
	}
	if op, ok := cmpOp(p.cur().Type); ok {
		t := p.advance()
		right, err := p.parsePipe()
		if err != nil {
			return nil, err
		}
		left = ast.NewBinaryExpr(op, left, right, t.Line, t.Col)
		if _, still := cmpOp(p.cur().Type); still {
			return nil, p.errf("comparison operators are non-associative")
		}
	}
	return left, nil
}

func cmpOp(t lexer.TokenType) (string, bool) {
	switch t {
	case lexer.OP_EQ:
		return "==", true
	case lexer.OP_NEQ:
		return "!=", true
	case lexer.OP_LT:
		return "<", true
	case lexer.OP_GT:
		return ">", true
	case lexer.OP_LE:
		return "<=", true
	case lexer.OP_GE:
		return ">=", true
	}
	return "", false
}

// pipe_expr ::= range_expr { "|>" pipe_rhs }
func (p *parser) parsePipe() (ast.Expr, error) {
	left, err := p.parseRange()
	if err != nil {
		return nil, err
	}
	for p.at(lexer.OP_PIPE) {
		op := p.advance()
		callee, args, err := p.parsePipeRHS()
		if err != nil {
			return nil, err
		}
		left = ast.NewPipeExpr(left, callee, args, op.Line, op.Col)
	}
	return left, nil
}

// pipe_rhs ::= pipe_name [ "(" [ args ] ")" ]
func (p *parser) parsePipeRHS() (ast.Expr, []ast.Expr, error) {
	start := p.cur()
	if !p.at(lexer.LOWER_IDENT) && !p.at(lexer.UPPER_IDENT) {
		return nil, nil, p.errf("expected pipe name, got %s", p.cur().Type)
	}
	name := p.advance().Lit
	for p.at(lexer.OP_DOT) {
		p.advance()
		if !p.at(lexer.LOWER_IDENT) && !p.at(lexer.UPPER_IDENT) {
			return nil, nil, p.errf("expected name after '.' in pipe")
		}
		name += "." + p.advance().Lit
	}
	callee := ast.NewVariableExpr(name, start.Line, start.Col)
	if p.at(lexer.LPAREN) {
		args, err := p.parseArgs()
		if err != nil {
			return nil, nil, err
		}
		return callee, args, nil
	}
	return callee, nil, nil
}

// range_expr ::= add_expr [ "to" add_expr ]  (non-assoc)
//
// §4.3: убывающий литеральный range (`1 to 0`, `5 to 1`) — ошибка
// парсинга. При вычисляемых границах — runtime :range_error.
func (p *parser) parseRange() (ast.Expr, error) {
	left, err := p.parseAdd()
	if err != nil {
		return nil, err
	}
	if p.at(lexer.KW_TO) {
		t := p.advance()
		right, err := p.parseAdd()
		if err != nil {
			return nil, err
		}
		if lv, lok := litIntValue(left); lok {
			if rv, rok := litIntValue(right); rok && lv > rv {
				return nil, p.errf("descending literal range %d to %d", lv, rv)
			}
		}
		left = ast.NewRangeExpr(left, right, t.Line, t.Col)
		if p.at(lexer.KW_TO) {
			return nil, p.errf("'to' is non-associative")
		}
	}
	return left, nil
}

// litIntValue возвращает int64 для литерального целого (возможно, под
// унарным минусом). Второе значение — true, если выражение является
// целочисленным литералом.
func litIntValue(e ast.Expr) (int64, bool) {
	switch x := e.(type) {
	case ast.LiteralExpr:
		s := x.ValueStr()
		if s == "" {
			return 0, false
		}
		for i := 0; i < len(s); i++ {
			c := s[i]
			if (c < '0' || c > '9') && c != '_' {
				return 0, false
			}
		}
		n, err := strconv.ParseInt(strings.ReplaceAll(s, "_", ""), 10, 64)
		if err != nil {
			return 0, false
		}
		return n, true
	case ast.UnaryExpr:
		if x.OpStr() == "-" {
			if inner, ok := litIntValue(x.Operand()); ok {
				return -inner, true
			}
		}
	}
	return 0, false
}

// add_expr ::= mul_expr { ( "+" | "-" ) mul_expr }
func (p *parser) parseAdd() (ast.Expr, error) {
	left, err := p.parseMul()
	if err != nil {
		return nil, err
	}
	for p.at(lexer.OP_PLUS) || p.at(lexer.OP_MINUS) {
		op := p.advance()
		right, err := p.parseMul()
		if err != nil {
			return nil, err
		}
		left = ast.NewBinaryExpr(op.Lit, left, right, op.Line, op.Col)
	}
	return left, nil
}

// mul_expr ::= unary_expr { ( "*" | "/" | "div" | "rem" ) unary_expr }
func (p *parser) parseMul() (ast.Expr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		var op string
		switch p.cur().Type {
		case lexer.OP_STAR:
			op = "*"
		case lexer.OP_SLASH:
			op = "/"
		case lexer.KW_DIV:
			op = "div"
		case lexer.KW_REM:
			op = "rem"
		default:
			return left, nil
		}
		t := p.advance()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = ast.NewBinaryExpr(op, left, right, t.Line, t.Col)
	}
}

// unary_expr ::= ( "-" | "not" ) unary_expr | pow_expr
func (p *parser) parseUnary() (ast.Expr, error) {
	if p.at(lexer.OP_MINUS) || p.at(lexer.KW_NOT) {
		op := p.advance()
		inner, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return ast.NewUnaryExpr(op.Lit, inner, op.Line, op.Col), nil
	}
	return p.parsePow()
}

// pow_expr ::= postfix_expr [ "**" unary_expr ]  (right-assoc)
func (p *parser) parsePow() (ast.Expr, error) {
	left, err := p.parsePostfix()
	if err != nil {
		return nil, err
	}
	if p.at(lexer.OP_POW) {
		op := p.advance()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return ast.NewBinaryExpr("**", left, right, op.Line, op.Col), nil
	}
	return left, nil
}

// postfix_expr ::= primary_expr { postfix_op }
// postfix_op ::= "." ( LOWER_IDENT | UPPER_IDENT ) | "(" [ args ] ")" | "[" expr "]"
func (p *parser) parsePostfix() (ast.Expr, error) {
	left, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for {
		switch p.cur().Type {
		case lexer.OP_DOT:
			dot := p.advance()
			if !p.at(lexer.LOWER_IDENT) && !p.at(lexer.UPPER_IDENT) {
				return nil, p.errf("expected name after '.'")
			}
			name := p.advance().Lit
			left = ast.NewMemberExpr(left, name, dot.Line, dot.Col)
		case lexer.LPAREN:
			args, err := p.parseArgs()
			if err != nil {
				return nil, err
			}
			left = ast.NewCallExpr(left, args, left.Pos(), left.End())
		case lexer.LBRACKET:
			p.advance()
			idx, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(lexer.RBRACKET, "']'"); err != nil {
				return nil, err
			}
			left = ast.NewIndexExpr(left, idx, left.Pos(), left.End())
		default:
			return left, nil
		}
	}
}

// args ::= arg { sep arg } [ sep ]
// arg ::= expr | ".." expr
func (p *parser) parseArgs() ([]ast.Expr, error) {
	if _, err := p.expect(lexer.LPAREN, "'('"); err != nil {
		return nil, err
	}
	var out []ast.Expr
	if p.at(lexer.RPAREN) {
		p.advance()
		return out, nil
	}
	for {
		if p.at(lexer.OP_DOTDOT) {
			dot := p.advance()
			e, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			out = append(out, ast.NewUnaryExpr("..", e, dot.Line, dot.Col))
		} else {
			e, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			out = append(out, e)
		}
		if p.match(lexer.COMMA) {
			if p.at(lexer.RPAREN) {
				break
			}
			continue
		}
		break
	}
	if _, err := p.expect(lexer.RPAREN, "')'"); err != nil {
		return nil, err
	}
	return out, nil
}

// primary_expr ::= literal | LOWER_IDENT | UPPER_IDENT | "(" expr ")"
//
//	| tuple | list | vector | map | record
//	| if | match | recv | with | trap
func (p *parser) parsePrimary() (ast.Expr, error) {
	t := p.cur()
	switch t.Type {
	case lexer.INT, lexer.FLOAT:
		p.advance()
		return ast.NewLiteralExpr(t.Lit, t.Line, t.Col), nil
	case lexer.STRING:
		p.advance()
		// scanString возвращает тело без кавычек (A4.1); AST хранит
		// канонический текст — с кавычками.
		return ast.NewLiteralExpr("\""+t.Lit+"\"", t.Line, t.Col), nil
	case lexer.BYTES:
		p.advance()
		return ast.NewBytesExpr(t.Lit, t.Line, t.Col), nil
	case lexer.REGEX:
		p.advance()
		return ast.NewRegexExpr(t.Lit, t.Line, t.Col), nil
	case lexer.DECIMAL:
		p.advance()
		return ast.NewDecimalExpr(t.Lit, t.Line, t.Col), nil
	case lexer.ATOM:
		p.advance()
		return ast.NewAtomExpr(t.Lit[1:], t.Line, t.Col), nil
	case lexer.KW_TRUE, lexer.KW_FALSE:
		p.advance()
		return ast.NewLiteralExpr(t.Lit, t.Line, t.Col), nil
	case lexer.LOWER_IDENT:
		p.advance()
		return ast.NewVariableExpr(t.Lit, t.Line, t.Col), nil
	case lexer.UPPER_IDENT:
		// Возможно: record_literal { name: expr } или просто конструктор.
		if p.peek(1).Type == lexer.LBRACE {
			p.advance()
			return p.parseRecordLiteral(t.Lit, t.Line, t.Col)
		}
		p.advance()
		return ast.NewVariableExpr(t.Lit, t.Line, t.Col), nil
	case lexer.LPAREN:
		return p.parseParenOrTuple()
	case lexer.LBRACKET:
		return p.parseListLiteral()
	case lexer.VEC_OPEN:
		return p.parseVectorLiteral()
	case lexer.MAP_OPEN:
		return p.parseMapLiteral()
	case lexer.LBRACE:
		return p.parseRecordLiteral("", t.Line, t.Col)
	case lexer.KW_IF:
		return p.parseIf()
	case lexer.KW_MATCH:
		return p.parseMatch()
	case lexer.KW_RECV:
		return p.parseRecv()
	case lexer.KW_WITH:
		return p.parseWith()
	case lexer.KW_TRAP:
		return p.parseTrap()
	}
	return nil, p.errf("expected expression, got %s", t.Type)
}

// parseParenOrTuple: (expr) — grouping; (e1, e2) — tuple; () — unit.
func (p *parser) parseParenOrTuple() (ast.Expr, error) {
	start := p.advance() // (
	if p.at(lexer.RPAREN) {
		p.advance()
		return ast.NewLiteralExpr("()", start.Line, start.Col), nil
	}
	first, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if p.at(lexer.COMMA) {
		// tuple
		elems := []ast.Expr{first}
		for p.match(lexer.COMMA) {
			if p.at(lexer.RPAREN) {
				break
			}
			e, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			elems = append(elems, e)
		}
		if _, err := p.expect(lexer.RPAREN, "')'"); err != nil {
			return nil, err
		}
		return ast.NewCallExpr(
			ast.NewVariableExpr("()", start.Line, start.Col), elems,
			start.Line, start.Col), nil
	}
	if _, err := p.expect(lexer.RPAREN, "')'"); err != nil {
		return nil, err
	}
	return ast.NewGroupingExpr(first, start.Line, start.Col), nil
}

// list_literal ::= "[" [ list_elem { sep list_elem } [ sep ] ] "]"
func (p *parser) parseListLiteral() (ast.Expr, error) {
	start := p.advance() // [
	elems, err := p.parseElems(lexer.RBRACKET)
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.RBRACKET, "']'"); err != nil {
		return nil, err
	}
	return ast.NewCallExpr(
		ast.NewVariableExpr("[]", start.Line, start.Col), elems,
		start.Line, start.Col), nil
}

func (p *parser) parseVectorLiteral() (ast.Expr, error) {
	start := p.advance() // %[
	elems, err := p.parseElems(lexer.RBRACKET)
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.RBRACKET, "']'"); err != nil {
		return nil, err
	}
	return ast.NewCallExpr(
		ast.NewVariableExpr("%[]", start.Line, start.Col), elems,
		start.Line, start.Col), nil
}

func (p *parser) parseMapLiteral() (ast.Expr, error) {
	start := p.advance() // %{
	if p.at(lexer.RBRACE) {
		p.advance()
		return ast.NewCallExpr(ast.NewVariableExpr("%{}", start.Line, start.Col), nil, start.Line, start.Col), nil
	}
	var elems []ast.Expr
	for {
		if p.at(lexer.OP_DOTDOT) {
			dot := p.advance()
			e, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			elems = append(elems, ast.NewUnaryExpr("..", e, dot.Line, dot.Col))
		} else {
			k, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(lexer.OP_FATARROW, "'=>'"); err != nil {
				return nil, err
			}
			v, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			elems = append(elems, ast.NewBinaryExpr("=>", k, v, k.Pos(), v.End()))
		}
		if p.match(lexer.COMMA) {
			if p.at(lexer.RBRACE) {
				break
			}
			continue
		}
		if p.match(lexer.NEWLINE) {
			if p.at(lexer.RBRACE) {
				break
			}
			continue
		}
		break
	}
	if _, err := p.expect(lexer.RBRACE, "'}'"); err != nil {
		return nil, err
	}
	return ast.NewCallExpr(
		ast.NewVariableExpr("%{}", start.Line, start.Col), elems,
		start.Line, start.Col), nil
}

func (p *parser) parseElems(closeToken lexer.TokenType) ([]ast.Expr, error) {
	var out []ast.Expr
	if p.at(closeToken) {
		return out, nil
	}
	for {
		if p.at(lexer.OP_DOTDOT) {
			dot := p.advance()
			e, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			out = append(out, ast.NewUnaryExpr("..", e, dot.Line, dot.Col))
		} else {
			e, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			out = append(out, e)
		}
		if p.match(lexer.COMMA) {
			if p.at(closeToken) {
				break
			}
			continue
		}
		if p.match(lexer.NEWLINE) {
			if p.at(closeToken) {
				break
			}
			continue
		}
		break
	}
	return out, nil
}

// record_literal ::= [ TypeName ] "{" [ record_elem { sep record_elem } [ sep ] ] "}"
func (p *parser) parseRecordLiteral(typ string, line, col int) (ast.Expr, error) {
	p.advance() // {
	if p.at(lexer.RBRACE) {
		p.advance()
		return ast.NewCallExpr(
			ast.NewVariableExpr(recordCtorName(typ), line, col), nil, line, col), nil
	}
	var elems []ast.Expr
	for {
		if p.at(lexer.OP_DOTDOT) {
			dot := p.advance()
			e, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			elems = append(elems, ast.NewUnaryExpr("..", e, dot.Line, dot.Col))
		} else {
			name, err := p.expect(lexer.LOWER_IDENT, "field name")
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(lexer.COLON, "':'"); err != nil {
				return nil, err
			}
			v, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			elems = append(elems, ast.NewBinaryExpr(":", ast.NewVariableExpr(name.Lit, name.Line, name.Col), v, name.Line, name.Col))
		}
		if p.match(lexer.COMMA) {
			if p.at(lexer.RBRACE) {
				break
			}
			continue
		}
		if p.match(lexer.NEWLINE) {
			if p.at(lexer.RBRACE) {
				break
			}
			continue
		}
		break
	}
	if _, err := p.expect(lexer.RBRACE, "'}'"); err != nil {
		return nil, err
	}
	return ast.NewCallExpr(
		ast.NewVariableExpr(recordCtorName(typ), line, col), elems, line, col), nil
}

func recordCtorName(typ string) string {
	if typ == "" {
		return "{}"
	}
	return typ + "{}"
}

// if_expr ::= "if" expr NEWLINE INDENT stmt_list DEDENT [ "else" ... ]
//
//	| "if" expr "then" expr "else" expr
func (p *parser) parseIf() (ast.Expr, error) {
	start := p.advance() // if
	cond, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	// Однострочная форма.
	if p.at(lexer.KW_THEN) {
		p.advance()
		thenE, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.KW_ELSE, "'else'"); err != nil {
			return nil, err
		}
		elseE, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		return ast.NewIfExpr(cond, thenE, nil, elseE, start.Line, start.Col), nil
	}
	// Блочная форма.
	if _, err := p.expect(lexer.NEWLINE, "NEWLINE after if condition"); err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.INDENT, "INDENT"); err != nil {
		return nil, err
	}
	stmts, err := p.parseStmtList(lexer.DEDENT)
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.DEDENT, "DEDENT"); err != nil {
		return nil, err
	}
	thenBlk := ast.NewBlockStmt(stmts, start.Line, start.Col)
	var elseBlk ast.Expr
	if p.at(lexer.KW_ELSE) {
		p.advance()
		if _, err := p.expect(lexer.NEWLINE, "NEWLINE after else"); err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.INDENT, "INDENT"); err != nil {
			return nil, err
		}
		elseStmts, err := p.parseStmtList(lexer.DEDENT)
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.DEDENT, "DEDENT"); err != nil {
			return nil, err
		}
		elseBlk = ast.NewBlockStmt(elseStmts, start.Line, start.Col)
	}
	return ast.NewIfExpr(cond, thenBlk, nil, elseBlk, start.Line, start.Col), nil
}

// match_expr ::= "match" expr NEWLINE INDENT match_branch+ DEDENT
func (p *parser) parseMatch() (ast.Expr, error) {
	start := p.advance() // match
	subject, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.NEWLINE, "NEWLINE after match subject"); err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.INDENT, "INDENT"); err != nil {
		return nil, err
	}
	var branches []ast.MatchBranchArg
	p.skipNewlines()
	for !p.at(lexer.DEDENT) && !p.at(lexer.EOF) {
		pat, err := p.parsePattern()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.OP_ARROW, "'->'"); err != nil {
			return nil, err
		}
		body, err := p.parseBranchBody()
		if err != nil {
			return nil, err
		}
		branches = append(branches, ast.MatchBranchArg{Pattern: pat, Body: body})
		p.skipNewlines()
	}
	if _, err := p.expect(lexer.DEDENT, "DEDENT"); err != nil {
		return nil, err
	}
	return ast.NewMatchExpr(subject, branches, start.Line, start.Col), nil
}

// Тело ветки: expr NEWLINE | NEWLINE INDENT stmt_list DEDENT
func (p *parser) parseBranchBody() (ast.Expr, error) {
	if p.at(lexer.NEWLINE) {
		p.advance()
		if _, err := p.expect(lexer.INDENT, "INDENT"); err != nil {
			return nil, err
		}
		stmts, err := p.parseStmtList(lexer.DEDENT)
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.DEDENT, "DEDENT"); err != nil {
			return nil, err
		}
		return ast.NewBlockStmt(stmts, p.cur().Line, p.cur().Col), nil
	}
	return p.parseExpr()
}

// with_expr ::= "with" NEWLINE INDENT (bind_stmt | stmt)+ DEDENT [ with_else ]
func (p *parser) parseWith() (ast.Expr, error) {
	start := p.advance() // with
	if _, err := p.expect(lexer.NEWLINE, "NEWLINE after with"); err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.INDENT, "INDENT"); err != nil {
		return nil, err
	}
	var items []ast.WithItemArg
	p.skipNewlines()
	// bind_stmt+ (pattern "<-" expr)
	for !p.at(lexer.DEDENT) && !p.at(lexer.EOF) {
		save := p.pos
		pat, err := p.parsePattern()
		if err != nil || !p.at(lexer.OP_LARROW) {
			p.pos = save
			break
		}
		p.advance() // <-
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		items = append(items, ast.WithItemArg{Pattern: pat, Expr: e})
		p.skipNewlines()
	}
	// body_stmt+ — оставшиеся стейтменты до DEDENT.
	stmts, err := p.parseStmtList(lexer.DEDENT)
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.DEDENT, "DEDENT"); err != nil {
		return nil, err
	}
	body := ast.NewBlockStmt(stmts, start.Line, start.Col)
	var elseBranches []ast.WithElseArg
	if p.at(lexer.KW_ELSE) {
		p.advance()
		if _, err := p.expect(lexer.NEWLINE, "NEWLINE"); err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.INDENT, "INDENT"); err != nil {
			return nil, err
		}
		p.skipNewlines()
		for !p.at(lexer.DEDENT) && !p.at(lexer.EOF) {
			pat, err := p.parsePattern()
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(lexer.OP_ARROW, "'->'"); err != nil {
				return nil, err
			}
			b, err := p.parseBranchBody()
			if err != nil {
				return nil, err
			}
			elseBranches = append(elseBranches, ast.WithElseArg{Pattern: pat, Body: b})
			p.skipNewlines()
		}
		if _, err := p.expect(lexer.DEDENT, "DEDENT"); err != nil {
			return nil, err
		}
	}
	return ast.NewWithExpr(items, body, elseBranches, start.Line, start.Col), nil
}

// parseTrap реализует обновлённую грамматику (v0.4.7, A2):
//
//	trap_expr ::= "trap" "(" expr ")"
//	            | "trap" NEWLINE INDENT trap_item+ DEDENT
//	trap_item ::= stmt | ensure_clause
func (p *parser) parseTrap() (ast.Expr, error) {
	start := p.advance() // trap

	// Инлайн-форма: trap(expr).
	if p.at(lexer.LPAREN) {
		p.advance()
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.RPAREN, "')'"); err != nil {
			return nil, err
		}
		return ast.NewTrapExpr(e, nil, nil, start.Line, start.Col), nil
	}

	// Блочная форма.
	if _, err := p.expect(lexer.NEWLINE, "NEWLINE after trap"); err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.INDENT, "INDENT"); err != nil {
		return nil, err
	}

	var stmts []ast.Stmt
	var ensures []ast.EnsureArg

	p.skipNewlines()
	for !p.at(lexer.DEDENT) && !p.at(lexer.EOF) {
		if p.at(lexer.KW_ENSURE) {
			p.advance()
			e, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			ensures = append(ensures, ast.EnsureArg{Expr: e})
			// Опциональное блочное тело ensure: NEWLINE INDENT stmt_list DEDENT.
			if p.at(lexer.NEWLINE) && p.peek(1).Type == lexer.INDENT {
				p.advance() // NEWLINE
				p.advance() // INDENT
				if _, err := p.parseStmtList(lexer.DEDENT); err != nil {
					return nil, err
				}
				if _, err := p.expect(lexer.DEDENT, "DEDENT"); err != nil {
					return nil, err
				}
			}
			p.skipNewlines()
			continue
		}
		s, err := p.parseStmt()
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, s)
		p.skipNewlines()
	}
	if _, err := p.expect(lexer.DEDENT, "DEDENT"); err != nil {
		return nil, err
	}

	body := ast.NewBlockStmt(stmts, start.Line, start.Col)
	return ast.NewTrapExpr(nil, body, ensures, start.Line, start.Col), nil
}

// parseRecv: после DEDENT-веточек грамматика допускает (KW_ELSE | KW_AFTER)
// без предварительного NEWLINE. Лексер эмитит DEDENT непосредственно
// перед клаузой-ключевым словом (см. §D.8), поэтому достаточно проверить
// at(KW_ELSE) / at(KW_AFTER). Дополнительно скипаем NEWLINE-разделители,
// если лексер их всё же эмитит (защита от новых edge-case'ов).
func (p *parser) parseRecv() (ast.Expr, error) {
	start := p.advance() // recv

	// Инлайн-форма.
	if !p.at(lexer.NEWLINE) {
		pat, err := p.parsePattern()
		if err != nil {
			return nil, err
		}
		var guard ast.Expr
		if p.match(lexer.KW_WHEN) {
			guard, err = p.parseExpr()
			if err != nil {
				return nil, err
			}
		}
		if _, err := p.expect(lexer.OP_ARROW, "'->'"); err != nil {
			return nil, err
		}
		body, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		return ast.NewRecvExpr(
			[]ast.RecvBranchArg{{Pattern: pat, Guard: guard, Body: body}},
			ast.RecvClauseArg{},
			start.Line, start.Col,
		), nil
	}

	// Блочная форма.
	if _, err := p.expect(lexer.NEWLINE, "NEWLINE after recv"); err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.INDENT, "INDENT"); err != nil {
		return nil, err
	}
	var branches []ast.RecvBranchArg
	p.skipNewlines()
	for !p.at(lexer.DEDENT) && !p.at(lexer.EOF) {
		pat, err := p.parsePattern()
		if err != nil {
			return nil, err
		}
		var guard ast.Expr
		if p.match(lexer.KW_WHEN) {
			guard, err = p.parseExpr()
			if err != nil {
				return nil, err
			}
		}
		if _, err := p.expect(lexer.OP_ARROW, "'->'"); err != nil {
			return nil, err
		}
		body, err := p.parseBranchBody()
		if err != nil {
			return nil, err
		}
		branches = append(branches, ast.RecvBranchArg{Pattern: pat, Guard: guard, Body: body})
		p.skipNewlines()
	}
	if _, err := p.expect(lexer.DEDENT, "DEDENT"); err != nil {
		return nil, err
	}

	// Защита: некоторые версии лексера могут эмитить лишний NEWLINE
	// между DEDENT и else/after.
	p.skipNewlines()

	var clauses ast.RecvClauseArg
	if p.at(lexer.KW_ELSE) {
		p.advance()
		name, err := p.expect(lexer.LOWER_IDENT, "binding after 'else'")
		if err != nil {
			return nil, err
		}
		clauses.ElseName = name.Lit
		if _, err := p.expect(lexer.NEWLINE, "NEWLINE"); err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.INDENT, "INDENT"); err != nil {
			return nil, err
		}
		stmts, err := p.parseStmtList(lexer.DEDENT)
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.DEDENT, "DEDENT"); err != nil {
			return nil, err
		}
		clauses.ElseBody = ast.NewBlockStmt(stmts, p.cur().Line, p.cur().Col)
	}

	p.skipNewlines()

	if p.at(lexer.KW_AFTER) {
		p.advance()
		t, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		clauses.AfterTime = t
		if _, err := p.expect(lexer.OP_ARROW, "'->'"); err != nil {
			return nil, err
		}
		b, err := p.parseBranchBody()
		if err != nil {
			return nil, err
		}
		clauses.AfterBody = b
	}
	return ast.NewRecvExpr(branches, clauses, start.Line, start.Col), nil
}
