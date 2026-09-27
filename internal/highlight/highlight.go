package highlight

// Span — участок исходника одного класса. Start и End — байтовые
// смещения, End не входит. Match — парная скобка к скобке под курсором.
type Span struct {
	Start, End int
	Class      Class
	Match      bool
}

// Result — классификация src.
type Result struct {
	Spans  []Span
	Guides []int // байтовые смещения пробелов-направляющих отступа
}

// Classify разбирает src. cursor — индекс руны под курсором; отрицательный
// — курсора нет. Неполный ввод (незакрытая строка, открытый блок) не
// паникует.
func Classify(src string, cursor int, env Env) Result {
	toks := scanSrc(src)
	ann := resolve(src, toks, env)
	brackets(src, toks, ann, cursor)
	var spans []Span
	for i, t := range toks {
		c, ok := spanClass(t, ann[i])
		if !ok {
			continue
		}
		spans = append(spans, Span{Start: t.start, End: t.end, Class: c, Match: ann[i].match})
	}
	extra, guides := indents(src, toks)
	spans = append(spans, extra...)
	return Result{Spans: spans, Guides: guides}
}

func spanClass(t token, a ann) (Class, bool) {
	if a.set {
		if a.class == "" {
			return "", false
		}
		return a.class, true
	}
	return t.base()
}

// Highlight — Classify и палитра: ANSI-текст с тем же числом строк.
// Направляющая заменяет пробел на символ той же ширины.
func Highlight(src string, cursor int, env Env, pal Palette) string {
	return pal.Paint(src, Classify(src, cursor, env))
}
