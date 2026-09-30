package sema_test

import "testing"

// T-231: голый член встроенного модуля с большой буквы (`Vec.Foo`) —
// не значение: у встроенных модулей нет конструкторов. sema.Check ловит
// это до компилятора.
func TestBareBuiltinModuleUpperMember(t *testing.T) {
	for _, src := range []string{
		"module Main\nfn main() ->\n    x = Vec.Foo\n    print(x)\n",
		"module Main\nfn main() ->\n    print(Map.Empty)\n",
		"module Main\nfn main() ->\n    Option.Nothing\n",
	} {
		wantErr(t, src, "built-in modules have no constructors (§7.6)")
	}
}

// Вызов и ссылка на функцию встроенного модуля — не затронуты.
func TestBuiltinModuleMemberValueAndCallOK(t *testing.T) {
	wantOK(t, "module Main\nfn main() ->\n    f = Vec.len\n    print(Vec.len(Vec.push(f, 1)))\n")
}
