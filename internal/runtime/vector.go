package runtime

import "math"

// Персистентный 32-арный trie (T-273, §4.4): элементы до tailOff лежат в
// дереве из полных листьев по 32, последние 1..32 — в tail. push пишет в
// tail без спуска по дереву — O(1) amortized; чтение и set копируют только
// путь от корня до листа — O(log₃₂ n). Неизменённые ветви разделяются
// между версиями.
//
// Форма дерева определяется одним числом cnt: листья внутри дерева всегда
// полные, внутренние узлы держат ровно своих детей, дерево плотное. У
// векторов равной длины форма одна, поэтому равенство и term order обходят
// их парами узлов и пропускают общие поддеревья.

const (
	vectorBits  = 5
	vectorWidth = 1 << vectorBits
	vectorMask  = vectorWidth - 1
)

// vectorNode — внутренний узел (kids) или лист (elems): непусто ровно одно
// поле. Лист внутри дерева держит vectorWidth элементов.
type vectorNode struct {
	kids  []*vectorNode
	elems []Value
}

// vectorTrie — неизменяемый вектор; nil — пустой. При cnt > 0 в tail лежит
// 1..vectorWidth элементов, в дереве — cnt - len(tail), кратное
// vectorWidth. shift — 5*(глубина дерева - 1): 0 — корень-лист, nil —
// дерева ещё нет.
//
// nan — в вектор попадал Float NaN или контейнер, который мог его
// содержать (maybeNaN). Равенство NaN нерефлексивно (§4.8), поэтому при
// nan общие поддеревья в равенстве не пропускаются.
type vectorTrie struct {
	cnt   int
	shift uint
	root  *vectorNode
	tail  []Value
	nan   bool
}

func (t *vectorTrie) len() int {
	if t == nil {
		return 0
	}
	return t.cnt
}

// tailOff — индекс первого элемента tail; он же число элементов в дереве.
func (t *vectorTrie) tailOff() int { return t.cnt - len(t.tail) }

// at — i-й элемент. Границы проверяет вызывающий: 0 <= i < len().
func (t *vectorTrie) at(i int) Value {
	if off := t.tailOff(); i >= off {
		return t.tail[i-off]
	}
	n := t.root
	for s := t.shift; s > 0; s -= vectorBits {
		n = n.kids[(i>>s)&vectorMask]
	}
	return n.elems[i&vectorMask]
}

// push — вектор с элементом v в конце: копируется tail (≤ 32 элемента)
// или, когда tail полон, путь до нового листа.
func (t *vectorTrie) push(v Value) *vectorTrie {
	if t == nil {
		return &vectorTrie{cnt: 1, tail: []Value{v}, nan: maybeNaN(v)}
	}
	nan := t.nan || maybeNaN(v)
	if len(t.tail) < vectorWidth {
		tail := make([]Value, len(t.tail)+1)
		copy(tail, t.tail)
		tail[len(t.tail)] = v
		return &vectorTrie{cnt: t.cnt + 1, shift: t.shift, root: t.root, tail: tail, nan: nan}
	}
	root, shift := pushLeaf(t.root, t.shift, t.tailOff(), &vectorNode{elems: t.tail})
	return &vectorTrie{cnt: t.cnt + 1, shift: shift, root: root, tail: []Value{v}, nan: nan}
}

// set — вектор, где i-й элемент заменён на v; копируется путь до листа или
// tail. Границы проверяет вызывающий: 0 <= i < len().
func (t *vectorTrie) set(i int, v Value) *vectorTrie {
	nan := t.nan || maybeNaN(v)
	if off := t.tailOff(); i >= off {
		tail := append([]Value(nil), t.tail...)
		tail[i-off] = v
		return &vectorTrie{cnt: t.cnt, shift: t.shift, root: t.root, tail: tail, nan: nan}
	}
	root := setPath(t.root, t.shift, i, v)
	return &vectorTrie{cnt: t.cnt, shift: t.shift, root: root, tail: t.tail, nan: nan}
}

// pushLeaf вставляет полный лист, первый элемент которого имеет индекс idx.
// Корень заполнен — дерево подрастает на уровень.
func pushLeaf(root *vectorNode, shift uint, idx int, leaf *vectorNode) (*vectorNode, uint) {
	if root == nil {
		return leaf, 0
	}
	if idx == vectorWidth<<shift {
		return &vectorNode{kids: []*vectorNode{root, newPath(shift, leaf)}}, shift + vectorBits
	}
	return insertLeaf(root, shift, idx, leaf), shift
}

// insertLeaf дописывает лист в дерево, где под idx ещё есть место. Вектор
// плотный, поэтому путь idx идёт по последнему ребёнку каждого узла или на
// одну позицию правее него; shift здесь всегда >= vectorBits.
func insertLeaf(n *vectorNode, shift uint, idx int, leaf *vectorNode) *vectorNode {
	k := (idx >> shift) & vectorMask
	kids := append(make([]*vectorNode, 0, k+1), n.kids...)
	if k < len(kids) {
		kids[k] = insertLeaf(kids[k], shift-vectorBits, idx, leaf)
	} else {
		kids = append(kids, newPath(shift-vectorBits, leaf))
	}
	return &vectorNode{kids: kids}
}

// newPath оборачивает лист в shift/vectorBits уровней узлов.
func newPath(shift uint, leaf *vectorNode) *vectorNode {
	if shift == 0 {
		return leaf
	}
	return &vectorNode{kids: []*vectorNode{newPath(shift-vectorBits, leaf)}}
}

// setPath копирует путь от узла n до листа с индексом i.
func setPath(n *vectorNode, shift uint, i int, v Value) *vectorNode {
	if shift == 0 {
		elems := append([]Value(nil), n.elems...)
		elems[i&vectorMask] = v
		return &vectorNode{elems: elems}
	}
	k := (i >> shift) & vectorMask
	kids := append([]*vectorNode(nil), n.kids...)
	kids[k] = setPath(kids[k], shift-vectorBits, i, v)
	return &vectorNode{kids: kids}
}

// each обходит элементы в порядке индексов; fn вернул false — стоп.
func (t *vectorTrie) each(fn func(Value) bool) bool {
	if t == nil {
		return true
	}
	if !eachLeaf(t.root, t.shift, fn) {
		return false
	}
	for _, v := range t.tail {
		if !fn(v) {
			return false
		}
	}
	return true
}

func eachLeaf(n *vectorNode, shift uint, fn func(Value) bool) bool {
	if n == nil {
		return true
	}
	if shift == 0 {
		for _, v := range n.elems {
			if !fn(v) {
				return false
			}
		}
		return true
	}
	for _, c := range n.kids {
		if !eachLeaf(c, shift-vectorBits, fn) {
			return false
		}
	}
	return true
}

// slice — элементы в порядке индексов. Вектор без дерева отдаёт свой tail
// без копии: значения неизменяемы (§0.13), а push и set его не меняют.
// Срез нельзя изменять.
func (t *vectorTrie) slice() []Value {
	if t == nil {
		return nil
	}
	if t.root == nil {
		return t.tail
	}
	out := make([]Value, 0, t.cnt)
	t.each(func(v Value) bool {
		out = append(out, v)
		return true
	})
	return out
}

// ---- равенство и порядок ----

// maybeNaN — значение может содержать Float NaN, для которого равенство
// нерефлексивно (§4.8: Equal(NaN, NaN) — false). Контейнеры считаются
// подозрительными без обхода: их элементы проверит само равенство.
// Замыкания и функции равны по identity, NaN в захваченных значениях
// равенства не касается.
func maybeNaN(v Value) bool {
	switch v.Kind {
	case KindFloat:
		return math.IsNaN(v.Float)
	case KindTuple, KindList, KindVector, KindMap, KindSet, KindVariant, KindRecord:
		return true
	}
	return false
}

// vectorEqual — поэлементное равенство векторов функцией eq. Общие
// поддеревья пропускаются, только когда ни в одном векторе нет NaN: иначе
// пропуск сделал бы `v == v` истиной там, где поэлементное сравнение даёт
// false (§4.8).
func vectorEqual(a, b *vectorTrie, eq func(x, y Value) bool) bool {
	if a.len() != b.len() {
		return false
	}
	if a == nil {
		return true
	}
	skip := !a.nan && !b.nan
	if a == b && skip {
		return true
	}
	if !nodeEqual(a.root, b.root, a.shift, skip, eq) {
		return false
	}
	for i, v := range a.tail {
		if !eq(v, b.tail[i]) {
			return false
		}
	}
	return true
}

// nodeEqual сравнивает узлы векторов равной длины: у них одна форма,
// поэтому дети и листья идут парами.
func nodeEqual(x, y *vectorNode, shift uint, skip bool, eq func(a, b Value) bool) bool {
	if x == nil {
		return true
	}
	if x == y && skip {
		return true
	}
	if shift == 0 {
		for i, v := range x.elems {
			if !eq(v, y.elems[i]) {
				return false
			}
		}
		return true
	}
	for i, c := range x.kids {
		if !nodeEqual(c, y.kids[i], shift-vectorBits, skip, eq) {
			return false
		}
	}
	return true
}

// vectorCompare — term order (§7.4 п.8): лексикографически. При равной
// длине — обход дерева с пропуском общих поддеревьев: Compare(NaN, NaN) — 0
// (cmpNaN), поэтому пропуск ничего не меняет. При разной длине — поэлементно
// по общему префиксу, затем более короткий меньше.
func vectorCompare(a, b *vectorTrie) (int, error) {
	if a.len() != b.len() {
		n := min(a.len(), b.len())
		for i := 0; i < n; i++ {
			if c, err := Compare(a.at(i), b.at(i)); err != nil || c != 0 {
				return c, err
			}
		}
		return cmpInt(a.len(), b.len()), nil
	}
	if a == nil || a == b {
		return 0, nil
	}
	if c, err := nodeCompare(a.root, b.root, a.shift); err != nil || c != 0 {
		return c, err
	}
	for i, v := range a.tail {
		c, err := Compare(v, b.tail[i])
		if err != nil || c != 0 {
			return c, err
		}
	}
	return 0, nil
}

func nodeCompare(x, y *vectorNode, shift uint) (int, error) {
	if x == nil || x == y {
		return 0, nil
	}
	if shift == 0 {
		for i, v := range x.elems {
			c, err := Compare(v, y.elems[i])
			if err != nil || c != 0 {
				return c, err
			}
		}
		return 0, nil
	}
	for i, kid := range x.kids {
		c, err := nodeCompare(kid, y.kids[i], shift-vectorBits)
		if err != nil || c != 0 {
			return c, err
		}
	}
	return 0, nil
}

// ---- построитель ----

// VectorBuilder собирает вектор за один проход: элементы пакуются в полные
// листья по 32, промежуточных версий вектора нет. Нулевое значение готово
// к работе.
type VectorBuilder struct {
	packed int // элементов в дереве
	shift  uint
	root   *vectorNode
	buf    []Value // 0..32 последних; собственный буфер построителя
	nan    bool
}

// NewVectorBuilder — построитель с местом под n элементов (больше 32 за
// раз не нужно: дальше элементы уходят в дерево листьями).
func NewVectorBuilder(n int) VectorBuilder {
	return VectorBuilder{buf: make([]Value, 0, min(max(n, 1), vectorWidth))}
}

// VectorBuilderFrom — построитель, продолжающий вектор v: его ветви
// разделяются с результатом, копируется только tail. Для прочих видов —
// пустой построитель.
func VectorBuilderFrom(v Value) VectorBuilder {
	if v.Kind != KindVector || v.vector == nil {
		return NewVectorBuilder(vectorWidth)
	}
	t := v.vector
	return VectorBuilder{
		packed: t.tailOff(),
		shift:  t.shift,
		root:   t.root,
		buf:    append(make([]Value, 0, vectorWidth), t.tail...),
		nan:    t.nan,
	}
}

// Add дописывает элемент в конец.
func (b *VectorBuilder) Add(v Value) {
	if len(b.buf) == vectorWidth {
		b.root, b.shift = pushLeaf(b.root, b.shift, b.packed, &vectorNode{elems: b.buf})
		b.packed += vectorWidth
		b.buf = make([]Value, 0, vectorWidth)
	}
	b.buf = append(b.buf, v)
	b.nan = b.nan || maybeNaN(v)
}

// Vector собирает вектор; построитель после этого не используется. Запас
// ёмкости буфера больше половины отбрасывается копией, чтобы короткий
// tail не удерживал блок под все 32 элемента.
func (b *VectorBuilder) Vector() Value {
	buf, root := b.buf, b.root
	b.buf, b.root = nil, nil
	if len(buf) == 0 {
		// Пустой буфер бывает только у построителя без Add: дерево
		// наполняется, лишь когда буфер переполнен.
		return Value{Kind: KindVector}
	}
	if len(buf) < cap(buf)/2 {
		buf = append([]Value(nil), buf...)
	}
	return Value{Kind: KindVector, vector: &vectorTrie{
		cnt: b.packed + len(buf), shift: b.shift, root: root, tail: buf, nan: b.nan,
	}}
}
