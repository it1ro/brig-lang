package parser

import (
	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/internal/lexer"
)

// type_decl ::= "type" UPPER_IDENT [ "<" generic_params ">" ] type_body
func (p *parser) parseTypeDecl() (ast.Decl, error) {
	kw, _ := p.expect(lexer.KW_TYPE, "'type'")
	name, err := p.expect(lexer.UPPER_IDENT, "type name")
	if err != nil {
		return nil, err
	}

	var generic []string
	if p.at(lexer.OP_LT) {
		p.advance()
		for {
			g, err := p.expect(lexer.UPPER_IDENT, "type variable")
			if err != nil {
				return nil, err
			}
			generic = append(generic, g.Lit)
			if !p.match(lexer.COMMA) {
				break
			}
		}
		if _, err := p.expect(lexer.OP_GT, "'>'"); err != nil {
			return nil, err
		}
	}

	// type_body ::= "=" type_expr | "{" variant_list "}" | "{" field_list "}"
	switch p.cur().Type {
	case lexer.OP_ASSIGN:
		p.advance()
		target, err := p.parseTypeExpr()
		if err != nil {
			return nil, err
		}
		return ast.NewAliasTypeDecl(name.Lit, generic, target, kw.Line, kw.Col), nil
	case lexer.LBRACE:
		return p.parseBracedTypeBody(name.Lit, generic, kw)
	}
	return nil, p.errf("expected '=' or '{' in type declaration")
}

// parseBracedTypeBody различает variant_list (Name(...) / Name) и field_list (name:).
func (p *parser) parseBracedTypeBody(name string, generic []string, kw lexer.Token) (ast.Decl, error) {
	p.advance() // '{'
	p.skipNewlines()

	if p.at(lexer.RBRACE) {
		p.advance()
		return ast.NewRecordTypeDecl(name, generic, nil, kw.Line, kw.Col), nil
	}

	isField := p.at(lexer.LOWER_IDENT) && p.peek(1).Type == lexer.COLON

	if isField {
		var fields []ast.FieldTypeArg
		for {
			fname, err := p.expect(lexer.LOWER_IDENT, "field name")
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(lexer.COLON, "':'"); err != nil {
				return nil, err
			}
			ft, err := p.parseTypeExpr()
			if err != nil {
				return nil, err
			}
			fields = append(fields, ast.FieldTypeArg{Name: fname.Lit, Field: ft})
			if !p.match(lexer.COMMA) {
				break
			}
			if p.at(lexer.RBRACE) {
				break
			}
		}
		p.skipNewlines()
		if _, err := p.expect(lexer.RBRACE, "'}'"); err != nil {
			return nil, err
		}
		return ast.NewRecordTypeDecl(name, generic, fields, kw.Line, kw.Col), nil
	}

	var variants []ast.VariantArg
	for {
		vname, err := p.expect(lexer.UPPER_IDENT, "variant name")
		if err != nil {
			return nil, err
		}
		v := ast.VariantArg{Name: vname.Lit}
		if p.match(lexer.LPAREN) {
			if !p.at(lexer.RPAREN) {
				for {
					ft, err := p.parseTypeExpr()
					if err != nil {
						return nil, err
					}
					v.Fields = append(v.Fields, ft)
					if !p.match(lexer.COMMA) {
						break
					}
				}
			}
			if _, err := p.expect(lexer.RPAREN, "')'"); err != nil {
				return nil, err
			}
		}
		variants = append(variants, v)
		if !p.match(lexer.COMMA) {
			break
		}
		if p.at(lexer.RBRACE) {
			break
		}
	}
	p.skipNewlines()
	if _, err := p.expect(lexer.RBRACE, "'}'"); err != nil {
		return nil, err
	}
	return ast.NewVariantTypeDecl(name, generic, variants, kw.Line, kw.Col), nil
}

// type_expr ::= type_primary [ "->" type_expr ]
func (p *parser) parseTypeExpr() (ast.Type, error) {
	start := p.cur()
	t, params, isTuple, err := p.parseTypePrimary()
	if err != nil {
		return nil, err
	}
	if p.at(lexer.OP_ARROW) {
		p.advance()
		result, err := p.parseTypeExpr()
		if err != nil {
			return nil, err
		}
		// (A, B) -> C — параметры A, B; (A) -> C — параметр A; () -> C — без параметров.
		var ps []ast.Type
		switch {
		case isTuple:
			ps = params
		case t == nil:
			ps = nil
		default:
			ps = []ast.Type{t}
		}
		return ast.NewFunctionType(ps, result, start.Line, start.Col), nil
	}
	return t, nil
}

// parseTypePrimary возвращает (основной тип, элементы-tuple, isTuple).
func (p *parser) parseTypePrimary() (ast.Type, []ast.Type, bool, error) {
	start := p.cur()

	switch p.cur().Type {
	case lexer.LPAREN:
		p.advance()
		if p.at(lexer.RPAREN) {
			p.advance()
			return ast.NewUnitType(start.Line, start.Col), nil, false, nil
		}
		var elems []ast.Type
		trailingComma := false
		for {
			te, err := p.parseTypeExpr()
			if err != nil {
				return nil, nil, false, err
			}
			elems = append(elems, te)
			if p.match(lexer.COMMA) {
				trailingComma = true
				if p.at(lexer.RPAREN) {
					break
				}
				continue
			}
			break
		}
		if _, err := p.expect(lexer.RPAREN, "')'"); err != nil {
			return nil, nil, false, err
		}
		// (A) без запятой — группировка.
		if len(elems) == 1 && !trailingComma {
			return elems[0], nil, false, nil
		}
		tt := ast.NewTupleType(elems, start.Line, start.Col)
		return tt, elems, true, nil

	case lexer.UPPER_IDENT:
		name := p.advance().Lit
		switch name {
		case "Int":
			return ast.NewIntType(start.Line, start.Col), nil, false, nil
		case "Float":
			return ast.NewFloatType(start.Line, start.Col), nil, false, nil
		case "Decimal":
			return ast.NewDecimalType(start.Line, start.Col), nil, false, nil
		case "Bool":
			return ast.NewBoolType(start.Line, start.Col), nil, false, nil
		case "Str":
			return ast.NewStrType(start.Line, start.Col), nil, false, nil
		case "Atom":
			return ast.NewAtomType(start.Line, start.Col), nil, false, nil
		case "Range":
			return ast.NewRangeType(start.Line, start.Col), nil, false, nil
		case "Pid":
			return ast.NewPidType(start.Line, start.Col), nil, false, nil
		case "Ref":
			return ast.NewRefType(start.Line, start.Col), nil, false, nil
		}

		var args []ast.Type
		if p.match(lexer.OP_LT) {
			for {
				a, err := p.parseTypeExpr()
				if err != nil {
					return nil, nil, false, err
				}
				args = append(args, a)
				if !p.match(lexer.COMMA) {
					break
				}
			}
			if _, err := p.expect(lexer.OP_GT, "'>'"); err != nil {
				return nil, nil, false, err
			}
		}

		switch name {
		case "List":
			if len(args) != 1 {
				return nil, nil, false, p.errf("List requires 1 type argument")
			}
			return ast.NewListType(args[0], start.Line, start.Col), nil, false, nil
		case "Vector":
			if len(args) != 1 {
				return nil, nil, false, p.errf("Vector requires 1 type argument")
			}
			return ast.NewVectorType(args[0], start.Line, start.Col), nil, false, nil
		case "Set":
			if len(args) != 1 {
				return nil, nil, false, p.errf("Set requires 1 type argument")
			}
			return ast.NewSetType(args[0], start.Line, start.Col), nil, false, nil
		case "Map":
			if len(args) != 2 {
				return nil, nil, false, p.errf("Map requires 2 type arguments")
			}
			return ast.NewMapType(args[0], args[1], start.Line, start.Col), nil, false, nil
		case "Option":
			if len(args) != 1 {
				return nil, nil, false, p.errf("Option requires 1 type argument")
			}
			return ast.NewOptionType(args[0], start.Line, start.Col), nil, false, nil
		case "Result":
			if len(args) != 2 {
				return nil, nil, false, p.errf("Result requires 2 type arguments")
			}
			return ast.NewResultType(args[0], args[1], start.Line, start.Col), nil, false, nil
		case "Tuple":
			return ast.NewTupleType(args, start.Line, start.Col), nil, false, nil
		}
		if len(args) > 0 {
			return ast.NewNominalType(name, nil, start.Line, start.Col), args, false, nil
		}
		return ast.NewNominalType(name, nil, start.Line, start.Col), nil, false, nil
	}
	return nil, nil, false, p.errf("expected type, got %s", p.cur().Type)
}
