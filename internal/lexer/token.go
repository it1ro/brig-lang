// Package lexer implements the Brig tokenizer and offside (indent-based)
// NEWLINE/INDENT/DEDENT generation.
//
// Spec: 01-language-design.md Part II F (Validation Stages) — A3 (tokens), A5 (offside).
package lexer

import "fmt"

// TokenType identifies a token kind (A3.1 full list).
type TokenType int

const (
	ILLEGAL TokenType = iota
	EOF
	NEWLINE
	INDENT
	DEDENT

	// Identifiers and literals.
	LOWER_IDENT
	UPPER_IDENT
	WILDCARD // "_" — only in pattern position
	ATOM     // :lower_ident[?]
	INT
	FLOAT
	DECIMAL
	STRING
	BYTES
	REGEX

	// Keywords (A3.1 reserved words).
	KW_FN
	KW_MATCH
	KW_RECV
	KW_WITH
	KW_ELSE
	KW_IF
	KW_THEN
	KW_AFTER
	KW_WHEN
	KW_ALIAS
	KW_IMPORT
	KW_MODULE
	KW_TYPE
	KW_ENSURE
	KW_TRAP
	KW_AND
	KW_OR
	KW_NOT
	KW_DIV
	KW_REM
	KW_TO
	KW_TRUE
	KW_FALSE
	KW_AS

	// Operators (longest-match: |>  **  ==  !=  <=  >=  ..  ->  <-  =>).
	OP_PIPE // |>
	OP_POW  // **
	OP_EQ   // ==
	OP_NEQ  // !=
	OP_LE   // <=
	OP_GE   // >=
	OP_DOTDOT
	OP_ARROW    // ->
	OP_LARROW   // <-
	OP_FATARROW // =>
	OP_PLUS
	OP_MINUS
	OP_STAR
	OP_SLASH
	OP_LT
	OP_GT
	OP_ASSIGN // =
	OP_DOT

	// Delimiters.
	LPAREN   // (
	RPAREN   // )
	LBRACKET // [
	RBRACKET // ]
	LBRACE   // {
	RBRACE   // }
	VEC_OPEN // %[
	MAP_OPEN // %{
	COMMA
	COLON
	SEMICOLON
)

// Token is a single lexeme with source position (1-based line/col).
type Token struct {
	Type TokenType
	Lit  string // raw text; for STRING/BYTES/REGEX/DECIMAL — body only
	Line int
	Col  int
}

func (t Token) String() string {
	return fmt.Sprintf("%s(%q)@%d:%d", t.Type, t.Lit, t.Line, t.Col)
}

// IsKeyword reports whether t is one of the reserved words.
func (t Token) IsKeyword() bool { return t.Type >= KW_FN && t.Type <= KW_AS }

// Postfixable reports whether t can precede a postfix `.` (x.name / x.f / x[i]).
func (t Token) Postfixable() bool {
	switch t.Type {
	case LOWER_IDENT, UPPER_IDENT, RPAREN, RBRACKET, RBRACE, INT, FLOAT,
		DECIMAL, STRING, BYTES, ATOM, KW_TRUE, KW_FALSE:
		return true
	}
	return false
}

func (t TokenType) String() string {
	switch t {
	case ILLEGAL:
		return "ILLEGAL"
	case EOF:
		return "EOF"
	case NEWLINE:
		return "NEWLINE"
	case INDENT:
		return "INDENT"
	case DEDENT:
		return "DEDENT"
	case LOWER_IDENT:
		return "LOWER_IDENT"
	case UPPER_IDENT:
		return "UPPER_IDENT"
	case WILDCARD:
		return "WILDCARD"
	case ATOM:
		return "ATOM"
	case INT:
		return "INT"
	case FLOAT:
		return "FLOAT"
	case DECIMAL:
		return "DECIMAL"
	case STRING:
		return "STRING"
	case BYTES:
		return "BYTES"
	case REGEX:
		return "REGEX"
	}
	if t >= KW_FN && t <= KW_AS {
		return keywordNames[t]
	}
	switch t {
	case OP_PIPE:
		return "|>"
	case OP_POW:
		return "**"
	case OP_EQ:
		return "=="
	case OP_NEQ:
		return "!="
	case OP_LE:
		return "<="
	case OP_GE:
		return ">="
	case OP_DOTDOT:
		return ".."
	case OP_ARROW:
		return "->"
	case OP_LARROW:
		return "<-"
	case OP_FATARROW:
		return "=>"
	case OP_PLUS:
		return "+"
	case OP_MINUS:
		return "-"
	case OP_STAR:
		return "*"
	case OP_SLASH:
		return "/"
	case OP_LT:
		return "<"
	case OP_GT:
		return ">"
	case OP_ASSIGN:
		return "="
	case OP_DOT:
		return "."
	case LPAREN:
		return "("
	case RPAREN:
		return ")"
	case LBRACKET:
		return "["
	case RBRACKET:
		return "]"
	case LBRACE:
		return "{"
	case RBRACE:
		return "}"
	case VEC_OPEN:
		return "%["
	case MAP_OPEN:
		return "%{"
	case COMMA:
		return ","
	case COLON:
		return ":"
	case SEMICOLON:
		return ";"
	}
	return fmt.Sprintf("TokenType(%d)", int(t))
}

// keywordNames maps keyword token kinds to their source text.
var keywordNames = map[TokenType]string{
	KW_FN: "fn", KW_MATCH: "match", KW_RECV: "recv", KW_WITH: "with",
	KW_ELSE: "else", KW_IF: "if", KW_THEN: "then", KW_AFTER: "after",
	KW_WHEN: "when", KW_ALIAS: "alias", KW_IMPORT: "import",
	KW_MODULE: "module", KW_TYPE: "type", KW_ENSURE: "ensure",
	KW_TRAP: "trap", KW_AND: "and", KW_OR: "or", KW_NOT: "not",
	KW_DIV: "div", KW_REM: "rem", KW_TO: "to", KW_TRUE: "true",
	KW_FALSE: "false", KW_AS: "as",
}

// keywords maps source word → keyword kind (A3.1, step 11a of A3.2).
var keywords = map[string]TokenType{
	"fn": KW_FN, "match": KW_MATCH, "recv": KW_RECV, "with": KW_WITH,
	"else": KW_ELSE, "if": KW_IF, "then": KW_THEN, "after": KW_AFTER,
	"when": KW_WHEN, "alias": KW_ALIAS, "import": KW_IMPORT,
	"module": KW_MODULE, "type": KW_TYPE, "ensure": KW_ENSURE,
	"trap": KW_TRAP, "and": KW_AND, "or": KW_OR, "not": KW_NOT,
	"div": KW_DIV, "rem": KW_REM, "to": KW_TO, "true": KW_TRUE,
	"false": KW_FALSE, "as": KW_AS,
}

// continuationOps — leading operators that make a line a continuation (A5.3).
var continuationOps = map[string]bool{
	"|>": true, "and": true, "or": true, "+": true, "-": true, "*": true,
	"/": true, "**": true, "div": true, "rem": true, "to": true,
	"==": true, "!=": true, "<": true, ">": true, "<=": true, ">=": true,
	"..": true,
}

// twoCharOps is checked before single-char operators (longest match, A3.2 §12).
var twoCharOps = []struct {
	text string
	typ  TokenType
}{
	{"|>", OP_PIPE},
	{"**", OP_POW},
	{"==", OP_EQ},
	{"!=", OP_NEQ},
	{"<=", OP_LE},
	{">=", OP_GE},
	{"..", OP_DOTDOT},
	{"->", OP_ARROW},
	{"<-", OP_LARROW},
	{"=>", OP_FATARROW},
}
