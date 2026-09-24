package parser

import (
	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/internal/lexer"
)

// pattern ::= pattern_atom [ "as" LOWER_IDENT ]
func (p *parser) parsePattern() (ast.Pattern, error) {
	start := p.cur()
	pat, err := p.parsePatternAtom()
	if err != nil {
		return nil, err
	}
	if p.at(lexer.KW_AS) {
		p.advance()
		id, err := p.expect(lexer.LOWER_IDENT, "identifier after 'as'")
		if err != nil {
			return nil, err
		}
		return ast.NewAsPat(pat, id.Lit, start.Line, start.Col), nil
	}
	return pat, nil
}

// pattern_atom ::= WILDCARD | LOWER_IDENT | literal
//
//	| constructor_pattern | tuple_pattern | list_pattern
//	| map_pattern | record_pattern | "(" pattern ")"
func (p *parser) parsePatternAtom() (ast.Pattern, error) {
	t := p.cur()
	switch t.Type {
	case lexer.WILDCARD:
		p.advance()
		return ast.NewWildcardPat(t.Line, t.Col), nil
	case lexer.LOWER_IDENT:
		p.advance()
		return ast.NewIdentPat(t.Lit, t.Line, t.Col), nil
	case lexer.INT, lexer.FLOAT, lexer.STRING, lexer.BYTES, lexer.REGEX, lexer.DECIMAL, lexer.ATOM:
		p.advance()
		return ast.NewLiteralPat(t.Lit, t.Line, t.Col), nil
	case lexer.KW_TRUE, lexer.KW_FALSE:
		p.advance()
		return ast.NewLiteralPat(t.Lit, t.Line, t.Col), nil
	case lexer.UPPER_IDENT:
		p.advance()
		// Record-паттерн: Upper "{"
		if p.at(lexer.LBRACE) {
			return p.parseRecordPatternRest(t.Lit, t.Line, t.Col)
		}
		// Constructor с аргументами.
		var args []ast.ConstructorPatArg
		if p.match(lexer.LPAREN) {
			if !p.at(lexer.RPAREN) {
				for {
					sub, err := p.parsePattern()
					if err != nil {
						return nil, err
					}
					args = append(args, ast.ConstructorPatArg{Pattern: sub})
					if !p.match(lexer.COMMA) {
						break
					}
				}
			}
			if _, err := p.expect(lexer.RPAREN, "')'"); err != nil {
				return nil, err
			}
		}
		return ast.NewConstructorPat(t.Lit, args, t.Line, t.Col), nil
	case lexer.LPAREN:
		return p.parseTupleOrGroupPattern()
	case lexer.LBRACKET:
		return p.parseListPattern()
	case lexer.MAP_OPEN:
		return p.parseMapPattern()
	case lexer.LBRACE:
		return p.parseRecordPatternRest("", t.Line, t.Col)
	}
	return nil, p.errf("expected pattern, got %s", t.Type)
}

// tuple_pattern ::= "(" pattern "," [ pattern { "," pattern } ] [ "," ] ")"
// Также допускает группировку: "(" pattern ")".
func (p *parser) parseTupleOrGroupPattern() (ast.Pattern, error) {
	start := p.advance() // (
	first, err := p.parsePattern()
	if err != nil {
		return nil, err
	}
	if p.at(lexer.COMMA) {
		elems := []ast.Pattern{first}
		for p.match(lexer.COMMA) {
			if p.at(lexer.RPAREN) {
				break
			}
			e, err := p.parsePattern()
			if err != nil {
				return nil, err
			}
			elems = append(elems, e)
		}
		if _, err := p.expect(lexer.RPAREN, "')'"); err != nil {
			return nil, err
		}
		return ast.NewTuplePattern(elems, start.Line, start.Col), nil
	}
	if _, err := p.expect(lexer.RPAREN, "')'"); err != nil {
		return nil, err
	}
	return first, nil
}

// list_pattern ::= "[" [ pattern { "," pattern } [ ".." [ name ] ] ] "]"
func (p *parser) parseListPattern() (ast.Pattern, error) {
	start := p.advance() // [
	var elems []ast.Pattern
	hasRest := false
	restName := ""
	for !p.at(lexer.RBRACKET) && !p.at(lexer.EOF) {
		if p.at(lexer.OP_DOTDOT) {
			p.advance()
			hasRest = true
			if p.at(lexer.LOWER_IDENT) {
				restName = p.advance().Lit
			}
			break
		}
		sub, err := p.parsePattern()
		if err != nil {
			return nil, err
		}
		elems = append(elems, sub)
		if !p.match(lexer.COMMA) {
			break
		}
	}
	if _, err := p.expect(lexer.RBRACKET, "']'"); err != nil {
		return nil, err
	}
	return ast.NewListPattern(elems, hasRest, restName, start.Line, start.Col), nil
}

// map_pattern ::= "%{" [ expr "=>" pattern { "," ... } ] "}"
func (p *parser) parseMapPattern() (ast.Pattern, error) {
	start := p.advance() // %{
	var pairs []ast.MapPairArg
	for !p.at(lexer.RBRACE) && !p.at(lexer.EOF) {
		k, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.OP_FATARROW, "'=>'"); err != nil {
			return nil, err
		}
		sub, err := p.parsePattern()
		if err != nil {
			return nil, err
		}
		pairs = append(pairs, ast.MapPairArg{Key: k, Pat: sub})
		if !p.match(lexer.COMMA) {
			break
		}
	}
	if _, err := p.expect(lexer.RBRACE, "'}'"); err != nil {
		return nil, err
	}
	return ast.NewMapPattern(pairs, start.Line, start.Col), nil
}

// record_pattern ::= [ UPPER_IDENT ] "{" [ field_pattern { "," field_pattern } ] "}"
func (p *parser) parseRecordPatternRest(typ string, line, col int) (ast.Pattern, error) {
	if _, err := p.expect(lexer.LBRACE, "'{'"); err != nil {
		return nil, err
	}
	var fields []ast.FieldPatArg
	for !p.at(lexer.RBRACE) && !p.at(lexer.EOF) {
		name, err := p.expect(lexer.LOWER_IDENT, "field name")
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.COLON, "':'"); err != nil {
			return nil, err
		}
		sub, err := p.parsePattern()
		if err != nil {
			return nil, err
		}
		fields = append(fields, ast.FieldPatArg{Name: name.Lit, Pat: sub})
		if !p.match(lexer.COMMA) {
			break
		}
	}
	if _, err := p.expect(lexer.RBRACE, "'}'"); err != nil {
		return nil, err
	}
	return ast.NewRecordPattern(typ, fields, line, col), nil
}
