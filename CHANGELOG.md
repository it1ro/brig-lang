## [unreleased]

### 🚀 Features

- *(lexer)* Add token set and offside-aware scanner (A3/A5)
- *(parser)* Add stage-1 stub with top-level checks (A9)
- *(tools)* Add check-examples runner per A2
- *(cli)* Add brig check, run, repl, version
- Add AST package (internal/ast/)
- *(parser)* Complete stage 2 recursive descent parser
- *(ast)* Add round-trip formatter and Program root
- *(parser)* [**breaking**] Make ensure a trap_item inside trap's INDENT block
- *(cli)* Expand subcommand descriptions for run repl check
- *(ast)* Add exported accessor interfaces for compiler
- *(parser)* Add compiler package for ast to bytecode
- *(runtime)* Add value model for primitives and collections
- *(vm)* Add chunk container for bytecode
- *(vm)* Add stack-based opcodes
- *(vm)* Add prelude and imports
- *(vm)* Add stack-based vm implementation
- *(cli)* Update subcommand descriptions
- *(parser)* Extend compiler to bytecode
- *(runtime)* Update value model
- *(vm)* Update chunk container
- *(vm)* Update opcodes
- *(vm)* Update vm implementation
- *(runtime)* Update value implementation
- *(vm)* Update chunk implementation
- *(vm)* Update vm implementation
- *(ast)* Update accessors
- *(parser)* Update compiler
- *(vm)* Update chunk
- *(vm)* Update opcodes
- *(vm)* Update vm
- *(parser)* Update compiler
- *(cli)* Update main
- *(ast)* Update accessors
- *(parser)* Update compiler
- *(vm)* Update chunk
- *(vm)* Update opcodes
- *(vm)* Update prelude
- *(vm)* Update vm
- *(vm)* Add pattern module
- *(vm)* Add scheduler module
- *(vm)* Update scheduler
- *(ast)* Update accessors
- *(parser)* Update compiler
- *(runtime)* Update value model
- *(vm)* Add pattern module
- *(vm)* Update prelude
- *(vm)* Update scheduler
- *(vm)* Update vm

### 🐛 Bug Fixes

- Exclude expected MVP errors from check-examples
- *(lexer)* Guard firstToken against slice bounds on trailing backslash
- *(ast)* Resolve revive and staticcheck lint errors
- *(vm)* Route non-native OpCall into current actor's frame stack
- *(runtime,vm,ast)* Small-int nil-deref and int-literal member access
- *(ast)* Add exported interface comments
- *(compiler)* Fix case order
- *(runtime)* Add KindUnit comment
- *(vm)* Suppress unused warning
- *(vm)* Add opcode comments
- *(vm)* Add PatWildcard comment
- *(vm)* Suppress staticcheck
- *(ast,compiler)* Seal literal accessor interfaces to disambiguate type switch

### 📚 Documentation

- Add initial language design v0.1.0
- Add language design v0.2.0
- Add critical review of v0.2.0 design
- Consolidate v0.2.0 review in v0.3.0 design
- Consolidate v0.4.0 decisions in design
- Remove obsolete AUDIT.md (v0.2.0 review)
- Rename syntax-design to language-design
- Update design to v0.4.1
- Update design to v0.4.2
- Update design to v0.4.3
- Add niche, performance and single-binary invariants
- Add language comparison (Go vs Rust)
- Add v0.4.4 design and formalization with EBNF
- Consolidate design v0.4.5 with Track A fixes
- Move design documents into docs/
- Add architecture notes
- Add brig.ebnf extracted from A1 formalization
- Consolidate 01.1-brig-formalization into 01-language-design.md
- *(examples)* Fix naming in docs
- Regenerate brig.ebnf from §16 v0.4.6 (A1, A6)
- Add arithmetic example brig file
- Add fibonacci example brig file
- Add hello example brig file
- Regenerate changelog
- Update architecture notes
- Add trap example
- Regenerate changelog
- Add actors example
- Regenerate changelog

### 🚜 Refactor

- Rename to Dreki; splink -> spawn_linked
- Rename language to Brig

### 🧪 Testing

- *(compiler)* Add compiler unit tests
- *(vm)* Add vm unit tests
- *(compiler)* Extend compiler tests
- *(runtime)* Add serialize tests
- *(compiler)* Update compiler tests
- *(compiler)* Update compiler tests
- *(compiler)* Update compiler tests
- *(vm)* Add scheduler tests
- *(compiler)* Update compiler tests
- *(vm)* Update scheduler tests
- *(compiler)* Update compiler tests

### ⚙️ Miscellaneous Tasks

- Scaffold Go module and tooling config
- *(git)* Add conventional commit and pre-push hooks
- Reserve ast, vm, runtime, prelude packages
- Add Makefile targets and GitHub Actions workflow
- Add project skills for brig subsystems
- *(make)* Add focus targets for stage 3
- *(git)* Fix lint exclusions and formatting for ast and parser
- *(build)* Add test-vm test-compiler run repl targets
- *(git)* Ignore coverage output
- Add test-vm test-compiler targets
- Disable gocritic linter

### 💼 Other

- A5.4 offside mini-blocks inside brackets
