package runtime

import (
	"math"
	"math/big"
	"math/bits"
	"sort"
	"sync/atomic"
)

// Персистентная хеш-таблица HAMT (T-272, §4.5–§4.6): ветвление 32, 64-битный
// хеш по 5 бит на уровень. Общие поддеревья разделяются между версиями;
// put и remove копируют только путь от корня до листа, O(log₃₂ n).
//
// Лист хранит полный хеш ключа. Ключи с одинаковым 64-битным хешем лежат
// в цепочке next одного листа. Форма дерева определяется набором хешей
// (remove сворачивает узел с единственным листом), поэтому равные по
// содержимому таблицы имеют одну форму и один порядок обхода — кроме
// порядка в цепочке коллизий полного хеша.

const (
	hamtBits = 5
	hamtMask = 1<<hamtBits - 1
)

type hamtLeaf struct {
	hash uint64
	key  Value
	val  Value
	next *hamtLeaf // цепочка ключей с тем же полным хешем
}

// hamtSlot — ровно одно из полей непусто.
type hamtSlot struct {
	leaf  *hamtLeaf
	child *hamtNode
}

type hamtNode struct {
	bitmap uint32
	slots  []hamtSlot
}

// hamt — неизменяемая таблица: корень и число пар. Кэши порядка
// заполняются лениво и не меняют содержимое.
type hamt struct {
	root *hamtNode
	n    int

	ents atomic.Pointer[[]MapEntry] // пары в term order (§7.4)
	keys atomic.Pointer[[]Value]    // ключи в term order, для Set
}

func (h *hamt) len() int {
	if h == nil {
		return 0
	}
	return h.n
}

func (h *hamt) get(hash uint64, key Value) *hamtLeaf {
	if h == nil {
		return nil
	}
	n := h.root
	for shift := uint(0); n != nil; shift += hamtBits {
		bit := uint32(1) << ((hash >> shift) & hamtMask)
		if n.bitmap&bit == 0 {
			return nil
		}
		s := n.slots[bits.OnesCount32(n.bitmap&(bit-1))]
		if s.child != nil {
			n = s.child
			continue
		}
		if s.leaf.hash != hash {
			return nil
		}
		for l := s.leaf; l != nil; l = l.next {
			if KeyEqual(l.key, key) {
				return l
			}
		}
		return nil
	}
	return nil
}

// put возвращает таблицу с парой key => val; существующий ключ заменяется
// (его прежний Key сохраняется ключом нового значения val, не ключа).
func (h *hamt) put(hash uint64, key, val Value) *hamt {
	if h == nil || h.root == nil {
		root := &hamtNode{}
		root = root.insert(0, &hamtLeaf{hash: hash, key: key, val: val})
		return &hamt{root: root, n: 1}
	}
	root, added := h.root.put(0, hash, key, val)
	n := h.n
	if added {
		n++
	}
	return &hamt{root: root, n: n}
}

func (n *hamtNode) put(shift uint, hash uint64, key, val Value) (*hamtNode, bool) {
	bit := uint32(1) << ((hash >> shift) & hamtMask)
	idx := bits.OnesCount32(n.bitmap & (bit - 1))
	if n.bitmap&bit == 0 {
		return n.withSlot(idx, bit, hamtSlot{leaf: &hamtLeaf{hash: hash, key: key, val: val}}), true
	}
	s := n.slots[idx]
	switch {
	case s.child != nil:
		c, added := s.child.put(shift+hamtBits, hash, key, val)
		return n.replaceSlot(idx, hamtSlot{child: c}), added
	case s.leaf.hash == hash:
		l, added := s.leaf.put(key, val)
		return n.replaceSlot(idx, hamtSlot{leaf: l}), added
	}
	nl := &hamtLeaf{hash: hash, key: key, val: val}
	return n.replaceSlot(idx, hamtSlot{child: joinLeaves(shift+hamtBits, s.leaf, nl)}), true
}

// insert кладёт лист в узел, где под его хеш нет слота.
func (n *hamtNode) insert(shift uint, l *hamtLeaf) *hamtNode {
	bit := uint32(1) << ((l.hash >> shift) & hamtMask)
	idx := bits.OnesCount32(n.bitmap & (bit - 1))
	return n.withSlot(idx, bit, hamtSlot{leaf: l})
}

// joinLeaves строит поддерево из двух листьев с разными хешами.
func joinLeaves(shift uint, a, b *hamtLeaf) *hamtNode {
	ia := uint32(a.hash>>shift) & hamtMask
	ib := uint32(b.hash>>shift) & hamtMask
	switch {
	case ia == ib:
		return &hamtNode{
			bitmap: 1 << ia,
			slots:  []hamtSlot{{child: joinLeaves(shift+hamtBits, a, b)}},
		}
	case ia < ib:
		return &hamtNode{bitmap: 1<<ia | 1<<ib, slots: []hamtSlot{{leaf: a}, {leaf: b}}}
	}
	return &hamtNode{bitmap: 1<<ia | 1<<ib, slots: []hamtSlot{{leaf: b}, {leaf: a}}}
}

func (n *hamtNode) withSlot(idx int, bit uint32, s hamtSlot) *hamtNode {
	slots := make([]hamtSlot, len(n.slots)+1)
	copy(slots, n.slots[:idx])
	slots[idx] = s
	copy(slots[idx+1:], n.slots[idx:])
	return &hamtNode{bitmap: n.bitmap | bit, slots: slots}
}

func (n *hamtNode) replaceSlot(idx int, s hamtSlot) *hamtNode {
	slots := make([]hamtSlot, len(n.slots))
	copy(slots, n.slots)
	slots[idx] = s
	return &hamtNode{bitmap: n.bitmap, slots: slots}
}

func (n *hamtNode) dropSlot(idx int, bit uint32) *hamtNode {
	slots := make([]hamtSlot, len(n.slots)-1)
	copy(slots, n.slots[:idx])
	copy(slots[idx:], n.slots[idx+1:])
	return &hamtNode{bitmap: n.bitmap &^ bit, slots: slots}
}

// put заменяет или дописывает ключ в цепочке с общим хешем.
func (l *hamtLeaf) put(key, val Value) (*hamtLeaf, bool) {
	if KeyEqual(l.key, key) {
		return &hamtLeaf{hash: l.hash, key: key, val: val, next: l.next}, false
	}
	if l.next == nil {
		return &hamtLeaf{hash: l.hash, key: l.key, val: l.val,
			next: &hamtLeaf{hash: l.hash, key: key, val: val}}, true
	}
	next, added := l.next.put(key, val)
	return &hamtLeaf{hash: l.hash, key: l.key, val: l.val, next: next}, added
}

// remove возвращает цепочку без key; second — нашёлся ли ключ.
func (l *hamtLeaf) remove(key Value) (*hamtLeaf, bool) {
	if l == nil {
		return nil, false
	}
	if KeyEqual(l.key, key) {
		return l.next, true
	}
	next, ok := l.next.remove(key)
	if !ok {
		return l, false
	}
	return &hamtLeaf{hash: l.hash, key: l.key, val: l.val, next: next}, true
}

// remove возвращает таблицу без key; ключа нет — саму h.
func (h *hamt) remove(hash uint64, key Value) *hamt {
	if h == nil || h.root == nil {
		return h
	}
	root, ok := h.root.remove(0, hash, key)
	if !ok {
		return h
	}
	return &hamt{root: root, n: h.n - 1}
}

func (n *hamtNode) remove(shift uint, hash uint64, key Value) (*hamtNode, bool) {
	bit := uint32(1) << ((hash >> shift) & hamtMask)
	if n.bitmap&bit == 0 {
		return n, false
	}
	idx := bits.OnesCount32(n.bitmap & (bit - 1))
	s := n.slots[idx]
	if s.child != nil {
		c, ok := s.child.remove(shift+hamtBits, hash, key)
		if !ok {
			return n, false
		}
		// Узел с единственным листом сворачивается в слот родителя.
		if len(c.slots) == 1 && c.slots[0].leaf != nil {
			return n.replaceSlot(idx, c.slots[0]), true
		}
		return n.replaceSlot(idx, hamtSlot{child: c}), true
	}
	if s.leaf.hash != hash {
		return n, false
	}
	l, ok := s.leaf.remove(key)
	if !ok {
		return n, false
	}
	if l != nil {
		return n.replaceSlot(idx, hamtSlot{leaf: l}), true
	}
	if len(n.slots) == 1 {
		return &hamtNode{}, true
	}
	return n.dropSlot(idx, bit), true
}

// each обходит пары в порядке хеша; fn вернул false — стоп.
func (h *hamt) each(fn func(*hamtLeaf) bool) {
	if h == nil || h.root == nil {
		return
	}
	h.root.each(fn)
}

func (n *hamtNode) each(fn func(*hamtLeaf) bool) bool {
	for _, s := range n.slots {
		if s.child != nil {
			if !s.child.each(fn) {
				return false
			}
			continue
		}
		for l := s.leaf; l != nil; l = l.next {
			if !fn(l) {
				return false
			}
		}
	}
	return true
}

// entries — пары в term order (§7.4), кэшируется. При несравнимых ключах
// их порядок задаёт обход хеша (стабильная сортировка). Срез нельзя менять.
func (h *hamt) entries() []MapEntry {
	if h == nil || h.n == 0 {
		return nil
	}
	if p := h.ents.Load(); p != nil {
		return *p
	}
	out := make([]MapEntry, 0, h.n)
	h.each(func(l *hamtLeaf) bool {
		out = append(out, MapEntry{Key: l.key, Val: l.val})
		return true
	})
	sort.SliceStable(out, func(i, j int) bool {
		c, err := Compare(out[i].Key, out[j].Key)
		return err == nil && c < 0
	})
	h.ents.Store(&out)
	return out
}

// keyList — ключи в term order, кэшируется. Срез нельзя менять.
func (h *hamt) keyList() []Value {
	if h == nil || h.n == 0 {
		return nil
	}
	if p := h.keys.Load(); p != nil {
		return *p
	}
	es := h.entries()
	out := make([]Value, len(es))
	for i, e := range es {
		out[i] = e.Key
	}
	h.keys.Store(&out)
	return out
}

// ---- хеш ключа ----

// hashKey согласован с KeyEqual: KeyEqual(a, b) ⇒ hashKey(a) == hashKey(b).
// Int и Float равны по точному численному значению, поэтому Float с целым
// значением хешируется как Int (-0.0 как 0). Decimal равен только Decimal
// и хешируется отдельным тегом. NaN не равен ничему, его хеш произволен.
func hashKey(v Value) uint64 {
	switch v.Kind {
	case KindUnit:
		return hashTag(v.Kind)
	case KindBool:
		if v.Bool {
			return hashTag(v.Kind) ^ 1
		}
		return hashTag(v.Kind)
	case KindInt:
		if v.IsSmall {
			return hashInt64(v.SmallInt)
		}
		return hashBigInt(v.AsBig())
	case KindFloat:
		return hashFloat(v.Float)
	case KindDecimal:
		h := hashTag(v.Kind)
		h = hashCombine(h, hashBigInt(v.Dec.Num()))
		return hashCombine(h, hashBigInt(v.Dec.Denom()))
	case KindStr:
		return hashCombine(hashTag(v.Kind), hashString(v.Str))
	case KindAtom:
		return hashCombine(hashTag(v.Kind), hashString(v.Atom))
	case KindBytes:
		return hashCombine(hashTag(v.Kind), hashString(string(v.Bytes)))
	case KindRange:
		h := hashCombine(hashTag(v.Kind), hashInt64(v.RangeStart))
		return hashCombine(h, hashInt64(v.RangeEnd))
	case KindTuple:
		return hashSeq(hashTag(v.Kind), v.Tuple)
	case KindVector:
		h := hashTag(v.Kind)
		v.vector.each(func(e Value) bool {
			h = hashCombine(h, hashKey(e))
			return true
		})
		return h
	case KindList:
		h := hashTag(v.Kind)
		for c := v.list; c != nil; c = c.tail {
			h = hashCombine(h, hashKey(c.head))
		}
		return h
	case KindMap:
		// Сумма хешей пар не зависит от порядка обхода.
		var sum uint64
		v.hamt.each(func(l *hamtLeaf) bool {
			sum += hashCombine(hashKey(l.key), hashKey(l.val))
			return true
		})
		return hashCombine(hashTag(v.Kind), sum)
	case KindSet:
		var sum uint64
		v.hamt.each(func(l *hamtLeaf) bool {
			sum += hashKey(l.key)
			return true
		})
		return hashCombine(hashTag(v.Kind), sum)
	case KindRecord:
		// Порядок полей не важен (equalRecords).
		var sum uint64
		for _, f := range v.Record.Fields {
			sum += hashCombine(hashString(f.Name), hashKey(f.Val))
		}
		h := hashCombine(hashTag(v.Kind), hashString(v.Record.Type))
		return hashCombine(h, sum)
	case KindVariant:
		h := hashCombine(hashTag(v.Kind), hashString(v.Variant.Type))
		h = hashCombine(h, hashString(v.Variant.Tag))
		return hashSeq(h, v.Variant.Args)
	case KindFunction:
		// Identity по имени и арности (equal).
		h := hashCombine(hashTag(v.Kind), hashString(v.Func.Name))
		return hashCombine(h, uint64(v.Func.Arity))
	case KindClosure:
		h := hashCombine(hashTag(v.Kind), hashString(v.ClosureVal.Name))
		return hashCombine(h, uint64(v.ClosureVal.Arity))
	case KindPid:
		return hashCombine(hashTag(v.Kind), uint64(v.Pid))
	case KindRef:
		return hashCombine(hashTag(v.Kind), uint64(v.Ref))
	case KindPort:
		return hashCombine(hashTag(v.Kind), uint64(v.Port.ID))
	}
	return 0
}

func hashTag(k Kind) uint64 { return mix64(uint64(k) + 0x9e3779b97f4a7c15) }

// mix64 — финализатор splitmix64.
func mix64(x uint64) uint64 {
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	x ^= x >> 31
	return x
}

func hashCombine(h, x uint64) uint64 {
	return mix64(h ^ (x + 0x9e3779b97f4a7c15 + h<<6 + h>>2))
}

func hashSeq(h uint64, vs []Value) uint64 {
	for _, e := range vs {
		h = hashCombine(h, hashKey(e))
	}
	return hashCombine(h, uint64(len(vs)))
}

// hashString — FNV-1a 64 с финализатором.
func hashString(s string) uint64 {
	h := uint64(14695981039346656037)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return mix64(h)
}

const numTag = 0x517cc1b727220a95

func hashInt64(i int64) uint64 { return mix64(uint64(i) ^ numTag) }

func hashBigInt(b *big.Int) uint64 {
	if b.IsInt64() {
		return hashInt64(b.Int64())
	}
	h := hashString(string(b.Bytes()))
	if b.Sign() < 0 {
		h = ^h
	}
	return hashCombine(numTag, h)
}

func hashFloat(f float64) uint64 {
	switch {
	case math.IsNaN(f):
		return hashTag(KindFloat)
	case math.IsInf(f, 0):
		return hashCombine(hashTag(KindFloat), math.Float64bits(f))
	case f == math.Trunc(f):
		if f >= -(1<<63) && f < 1<<63 {
			return hashInt64(int64(f))
		}
		// Целое вне int64: точное десятичное представление.
		i, _ := new(big.Int).SetString(new(big.Float).SetFloat64(f).Text('f', 0), 10)
		return hashBigInt(i)
	}
	return hashCombine(hashTag(KindFloat), math.Float64bits(f))
}
