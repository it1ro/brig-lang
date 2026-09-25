# Register VM design

Нормативный дизайн регистровой VM (Sprint 7). Сигнатуры и структуры — контракт;
тела функций в примерах на Go — ориентир реализации. Источник истины по VM
ниже `docs/01-language-design.md` и `brig.ebnf` (см. иерархию в
`.claude/skills/brig-overview`). Краткий обзор слоёв — `docs/architecture.md`.

---

### Сводка решений

| Вопрос       | Решение                                                                                             |
| ------------ | --------------------------------------------------------------------------------------------------- |
| Инструкция   | `type Instr uint32`: `[C:8 \| B:8 \| A:8 \| op:8]`, варианты ABC / ABx / AsBx                       |
| Регистры     | 256 на кадр; размер `regs` фиксирован **на функцию** (`Chunk.NumRegs`)                              |
| Вызов        | `CALL A B C`: callee в `R[A]`, аргументы в `R[A+1..A+B]`, результат в `R[C]`                        |
| TCO          | явный `TAILCALL A B`, который эмитит компилятор; `isTailCall` удаляется                             |
| trap         | `TRAPBEGIN A sBx`: `A` — регистр, куда падает ошибка; `stackLen` удаляется                          |
| Паттерны     | `MATCHLOCAL A Bx` + обязательный следующий `JMP` (fail)                                             |
| Аллокатор    | bump-указатель со стековой дисциплиной (`nextReg`, `releaseToMark`); `freeRegs` и spill не вводятся |
| Escape-форма | нет                                                                                                 |

### Контракты, которые дизайн сохраняет (фиксируются явно)

Часть этих поведений в регистровой VM «случайна». Они наблюдаемы, поэтому становятся контрактом.

- **K-1. Порядок вычисления.** Callee вычисляется раньше аргументов. Аргументы, операнды и элементы литералов вычисляются слева направо. В `%{}` для каждой пары сначала ключ, затем значение. `and`/`or` — короткое замыкание.
- **K-2. Условные переходы.** `JMPIFNOT` прыгает **только** на `Bool(false)`, `JMPIF` — только на `Bool(true)`. Не-Bool «проваливается» (в `if` это truthy). Поэтому `and`/`or` возвращают значение операнда, а не обязательно `Bool` (текущий `compileAndOr` + `OpJumpFalse/True`).
- **K-3. Что ловит `trap`.** Только `*ErrRaise`. Ошибки арности, `arithErr`, `runtime.Compare`, `not` не-Bool — `fmt.Errorf`, фатальны для актора и `trap` их не видит.
- **K-4. Редукции.** Одна редукция — это `CALL` в байткод-функцию, `TAILCALL` в байткод-функцию, `RETURN` и шаг unwind. Вызов native редукций не тратит.
- **K-5. Аргументы native.** Native получает свежий слайс (`runtime.List(args...)`, `Variant("Some", args...)` его удерживают). Слайс на окно регистров передавать нельзя.
- **K-6. recv.** Сначала `downMsgs`, потом `mailbox`. Дедлайн живёт в `Actor.recvDeadline` и сбрасывается при взятии сообщения. Пока актор заблокирован, `ip` стоит на `RECVTAKE`.
- **K-7. Замыкания.** Лямбда всегда даёт `KindClosure` (даже с 0 захватов), захваты — снимок по значению (N12, `TestClosureCapture`). Локальная `fn` остаётся глобальной `Function` с именем `outer$name`.
- **K-8 (вне рамок).** Компилируется только первый клоз `fn`, параметры-паттерны не поддерживаются, локальная `fn` не захватывает локали, `match`/`with` не компилируются, `when` в `recv` игнорируется. Дизайн это не чинит.
- **Пробелы вне K-8 (A-F8).** Следующие фичи из спеки не входят в список K-8, но в
  текущей реализации тоже отсутствуют или неполны — их нельзя считать
  «закрытыми Sprint 7», и молчаливый неверный результат недопустим:
  - pipe `|>` (§7.5);
  - record-литералы;
  - `link` (§12.6);
  - `Sys.args()` (§16 Must);
  - `mailbox_size()` без аргументов (§12.6).
  Интерполяция строк — отдельный finding (S-F1): молчаливый неверный результат.

### Сознательные исправления (единственные отклонения от регистрового поведения)

Все пять — латентные баги, которых не касается ни один существующий тест. Каждый получает новый тест в `internal/compiler/regvm_test.go`.

| №   | Было                                                                                                                | Стало                                                                                     | Обоснование                                                        |
| --- | ------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| D-1 | Variadic `fn f(x, ..rest)`: `frameFromFn` копирует аргументы в слоты как есть, `rest` — «сырой» второй аргумент     | `rest` = `List` остальных аргументов                                                      | §6.3; компилятор уже помечает `Arity == -1`                        |
| D-2 | Native с неверным числом аргументов → Go-panic (`args[0]`)                                                          | Фатальная ошибка `function_clause` (не catchable, как K-3)                                | Panic → диагностируемая ошибка                                     |
| D-3 | `recv … else msg`: имя `msg` не связывается (`undefined: msg`)                                                      | `msg` — алиас регистра сообщения                                                          | §12.4                                                              |
| D-4 | Одна плоская область на функцию (переменные блока видны после блока); upvalue только на 1 уровень вложенности       | Настоящие лексические области; upvalue рекурсивно (`upvalueInfo.isLocal` уже так задуман) | §6.6; повторное использование регистров требует настоящих областей |
| D-5 | `trap` с `ensure` различает успех и ошибку сравнением с атомом `:no_error`, поэтому `raise(:no_error)` даёт `Ok(…)` | Флаг-регистр `Bool`                                                                       | §10.3                                                              |

---

### 1. Формат инструкции

**Размер: 4 байта, `type Instr uint32`.** `Chunk.Code` становится `[]Instr`, `ip` — индекс инструкции (не байтовое смещение).

Причины:

- Убирается разнобой 1/3/5 байт и `EmitTwo`/`Operand2Pos`/`opSize`.
- Любая инструкция декодируется одним чтением.
- Патчинг переходов — запись одного слова.
- CALL с тремя операндами (callee-окно, argc, dst) помещается без escape.

```go
// Раскладка (старшие биты слева):
//
//   31      24 23      16 15       8 7        0
//  +----------+----------+----------+----------+
//  |    C     |    B     |    A     |    op    |   ABC
//  +----------+----------+----------+----------+
//  |      Bx / sBx       |    A     |    op    |   ABx / AsBx
//  +---------------------+----------+----------+
type Instr uint32

func (i Instr) Op() OpCode { return OpCode(i) }            // усечение до байта
func (i Instr) A() int     { return int(i >> 8 & 0xFF) }
func (i Instr) B() int     { return int(i >> 16 & 0xFF) }
func (i Instr) C() int     { return int(i >> 24) }
func (i Instr) Bx() int    { return int(i >> 16) }         // uint16
func (i Instr) SBx() int   { return int(int16(i >> 16)) }  // знаковый

func ABC(op OpCode, a, b, c int) Instr {
 return Instr(op) | Instr(a)<<8 | Instr(b)<<16 | Instr(c)<<24
}
func ABx(op OpCode, a, bx int) Instr { return Instr(op) | Instr(a)<<8 | Instr(bx)<<16 }
func AsBx(op OpCode, a, sbx int) Instr {
 return Instr(op) | Instr(a)<<8 | Instr(uint16(int16(sbx)))<<16
}
```

**Лимиты**

| Ресурс                          | Лимит                        | При переполнении                                        |
| ------------------------------- | ---------------------------- | ------------------------------------------------------- |
| Регистры на функцию             | 256 (`vm.MaxRegs`, R0..R255) | ошибка компиляции, см. §7                               |
| Константы на функцию            | 65 536 (`Bx`)                | ошибка `function %q: more than 65536 constants`         |
| Паттерны на функцию             | 65 536 (`Bx`)                | то же                                                   |
| Смещение перехода               | −32768..+32767 инструкций    | ошибка `function %q too large: jump out of int16 range` |
| Аргументов в `CALL`             | `B` ≤ 255 и `A+B` ≤ 255      | ошибка компиляции                                       |
| Элементов в `TUPLE/LIST/VECTOR` | `C` ≤ 255 и `B+C` ≤ 256      | ошибка компиляции                                       |
| Пар в `MAP`                     | `2C` ≤ 255                   | ошибка компиляции                                       |
| Upvalue на замыкание            | 256 (`GETUPVAL B`)           | ошибка компиляции                                       |

**Почему 256 регистров.** Столько влезает в 8-битные `A/B/C`. Это же значение `MaxLocals` (Sprint 6.2 поднял его с 64 до 256 под реальные тесты). Новый лимит считает живые регистры (регистровая дисциплина), а старый считал все слоты, когда-либо объявленные в функции. Для локалей и временных он строго мягче. Жёстче он только для широких вызовов и литералов: раньше их элементы жили на операнд-стеке до 65 535 штук, теперь им нужно окно регистров.

**Смещения переходов.** `sBx` — `int16`, отсчёт от **следующей** инструкции: `target = ip + 1 + sBx`. Форма знаковая, чтобы будущие обратные переходы не потребовали второго декодера, хотя сегодня все переходы прямые. Формат применим к `JMP`, `JMPIF`, `JMPIFNOT`, `TRAPBEGIN` (обработчик) и `RECVTAKE` (ветка `after`).

**`MATCHLOCAL`.** Ему нужны три величины: регистр, индекс паттерна и fail-адрес. Одна инструкция не вмещает `Bx` и переход одновременно, поэтому используется пара «проверка + следующий `JMP`». Из Lua 5.x перенимается именно это: инструкция-проверка содержит только условие, а адрес перехода лежит в соседней `JMP`.

```
MATCHLOCAL A Bx     ; if MatchPattern(R[A], Patterns[Bx], regs) { ip += 2 } else { ip += 1 }
JMP        sBx      ; fail-переход, выполняется только при неудаче матча
```

Компилятор всегда эмитит пару целиком (`Verify` проверяет), `FailAddr` из `CompiledPattern` удаляется.

**Multi-операндные инструкции**

| Инструкция                            | Кодирование                                                           |
| ------------------------------------- | --------------------------------------------------------------------- |
| `CALL A B C`                          | `R[C] = R[A](R[A+1..A+B])`                                            |
| `TUPLE/LIST/VECTOR A B C`             | `R[A] = ctor(R[B..B+C-1])`                                            |
| `MAP A B C` (старый `NEWMAP`/`OpMap`) | `R[A]` = мапа из `C` пар, лежащих в `R[B..B+2C-1]` как `k1,v1,k2,v2…` |
| `MAKECLOSURE A B C`                   | `R[A]` = замыкание над функцией из `R[B]` и захватами `R[B+1..B+C]`   |
| `RECVTAKE A sBx`                      | `R[A]` = сообщение; `sBx` — переход на `after`                        |
| `SEND A B C`                          | `R[A] = send(pid=R[B], msg=R[C])`                                     |

**Escape-формы нет** (ни `EXTRAARG`, ни 8-байтных инструкций). Лимиты выше на два порядка превосходят любую программу из `examples/` и тестов. Escape потребовал бы второй путь декодирования в VM, `Verify` и дизассемблере ради случая, которого не существует. При достижении лимита — внятная ошибка компиляции.

---

### 2. Модель кадра

```go
// Frame — кадр вызова. Значения живут в regs; стека операндов нет.
type Frame struct {
 chunk    *Chunk          // код, константы, паттерны (без &Function на каждый вызов)
 name     string          // для диагностики
 ip       int             // индекс инструкции в chunk.Code
 regs     []runtime.Value // len == chunk.NumRegs
 captures []runtime.Value // значения upvalue замыкания (только чтение)
 handlers []trapHandler
 callDst  int             // регистр В ЭТОМ кадре, куда положить результат
                          // вызванного им кадра (записывается инструкцией CALL)
}

// trapHandler — активный trap (§10.2). Значения не восстанавливаются:
// регистры зафиксированы, временные значения мертвы по построению компилятора.
type trapHandler struct {
 ip     int // абсолютный индекс инструкции-обработчика
 errReg int // регистр, куда кладётся значение raise
}
```

- **Что заменило `stack` и `locals`.** Один слайс `regs`. Параметры лежат в `R0..R(NumParams-1)`, за ними локали и временные.
- **Размер `regs`.** Динамический **между функциями**, фиксированный **внутри**: `Chunk.NumRegs` — high-water mark аллокатора. Старый кадр выделял 256 `Value` (по полям `value.go` около 290 байт каждый, порядка 70 КБ на вызов); теперь выделяется ровно `NumRegs`.
- **Жизненный цикл.**
  - `regs` создаётся в `frameFromFn` (`CALL`, `Spawn`).
  - `TAILCALL` переиспользует слайс, если `NumRegs` новой функции ≤ `cap(f.regs)`, иначе создаёт новый.
  - Старый массив освобождает GC.
  - При снятии кадра в `runSlice`/`callSync` перед усечением `a.frames` нужно писать `nil` в снимаемый слот, иначе кадр висит в backing-массиве.
- **`trapHandler.stackLen` удаляется.** Усечение стека нужно было, чтобы выбросить промежуточные значения. При регистровой модели промежуточные значения — это регистры выше живых, компилятор после обработчика их не читает. Вместо `stackLen` появляется `errReg`.
- **Возврат результата.** Вместо `caller.stack = append(caller.stack, res)` теперь `caller.regs[caller.callDst] = res`. Это три места: `runSlice`, `callSync`, `tryUnwindRaise`, где `parent.regs[h.errReg] = rerr.Val`.
- **Поля `Actor` не меняются.** Меняется только `Frame`.

```go
type callee struct {
 chunk    *Chunk
 name     string
 captures []runtime.Value
}

// resolveCallee разбирает Function/Closure с байткод-телом.
// Тексты ошибок сохранены дословно из текущего frameFromFn.
func resolveCallee(fn runtime.Value) (callee, error)

// checkArity: variadic — argc >= NumParams-1, иначе argc == NumParams.
func checkArity(c callee, argc int) error // (:function_clause, (spawn NAME, N args))

// bindArgs раскладывает args по регистрам параметров и НЕ удерживает args.
// Безопасна при перекрытии args и regs (TAILCALL): rest копируется первым.
func bindArgs(regs []runtime.Value, ch *Chunk, args []runtime.Value) {
 if !ch.Variadic {
  copy(regs, args) // copy == memmove, перекрытие допустимо
  return
 }
 fixed := ch.NumParams - 1
 rest := make([]runtime.Value, len(args)-fixed)
 copy(rest, args[fixed:])
 copy(regs, args[:fixed])
 regs[fixed] = runtime.List(rest...)
}

func frameFromFn(fn runtime.Value, args []runtime.Value) (*Frame, error) {
 c, err := resolveCallee(fn)
 if err != nil {
  return nil, err
 }
 if err := checkArity(c, len(args)); err != nil {
  return nil, err
 }
 regs := make([]runtime.Value, c.chunk.NumRegs)
 bindArgs(regs, c.chunk, args)
 return &Frame{chunk: c.chunk, name: c.name, regs: regs, captures: c.captures}, nil
}
```

`Chunk` получает поля `NumRegs`, `NumParams`, `Variadic` (§8). `runtime.FuncValue` не трогаем: информация о variadic лежит в чанке, а `Function.Arity` остаётся `-1`.

---

### 3. Соглашение о вызовах

| Вопрос         | Решение                                                                                                                                                                                                                                   |
| -------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Где callee     | `R[A]`                                                                                                                                                                                                                                    |
| Где аргументы  | `R[A+1 .. A+B]`, последовательные регистры                                                                                                                                                                                                |
| Куда результат | `R[C]` (для `TAILCALL` результата в регистре нет: он уходит вызывающему через `callDst` вызывающего)                                                                                                                                      |
| Arity          | `B` = число аргументов (0..255). Проверка на рантайме против `Chunk.NumParams`, не в байткоде                                                                                                                                             |
| Native         | Тот же layout. Различаются в `CALL` по `callee.Func.IsNative`. Аргументы копируются в свежий слайс (K-5), результат — в `R[C]`. При `Arity >= 0 && argc != Arity` — фатальная ошибка (D-2)                                                |
| Variadic       | Компилятор: `Chunk.Variadic = true`, `NumParams = len(params)`, `Function.Arity = -1`. VM: первые `NumParams-1` аргументов в свои регистры, остальные — `List` в регистр `NumParams-1` (D-1). Native с `Arity == -1` получают сырой слайс |

Для байткод-callee аргументы копируются напрямую из окна вызывающего в новые `regs` без промежуточного слайса.

```go
case OpCall:
 base, argc, dst := i.A(), i.B(), i.C()
 cv := regs[base]
 args := regs[base+1 : base+1+argc]
 if cv.Kind == runtime.KindFunction && cv.Func != nil && cv.Func.IsNative {
  if ar := cv.Func.Arity; ar >= 0 && ar != argc {
   return fail(fmt.Errorf("(:function_clause, (%s, %d args))", cv.Func.Name, argc))
  }
  fresh := append([]runtime.Value(nil), args...) // K-5
  r, err := cv.Func.Native(s.vm, fresh)
  if err != nil {
   if f.catch(err) {
    continue
   }
   return fail(err)
  }
  regs[dst] = r
  f.ip++
  continue
 }
 nf, err := frameFromFn(cv, args) // копирует args в новые regs
 if err != nil {
  if f.catch(err) {
   continue
  }
  return fail(err)
 }
 f.callDst = dst
 f.ip++
 a.frames = append(a.frames, nf)
 return stepContinue
```

---

### 4. TCO

**Явный `TAILCALL A B`, его эмитит компилятор.** Эвристика `isTailCall` **удаляется**. Причины:

- Хвостовость — свойство синтаксического положения, компилятор знает её точно.
- Эвристика по «`CALL; RETURN` или `CALL; JMP→RETURN`» зависела от того, как случайно сошёлся layout `compileIf`/`compileRecv`.
- В регистровой модели после `CALL A B C` нет обязательного `RETURN`, так что распознавать было бы нечего.

Компилятор передаёт хвостовость через `dest.tail` (§7). Хвостовые позиции, для которых сегодня есть код в компиляторе:

| #   | Позиция                                                   | Замечание                                                                       |
| --- | --------------------------------------------------------- | ------------------------------------------------------------------------------- |
| 1   | Последний стейтмент-выражение тела `fn`                   | Включая `main` и REPL-строку                                                    |
| 2   | Тело лямбды: короткой, пустой, последний стейтмент полной |                                                                                 |
| 3   | Тело локальной `fn`                                       |                                                                                 |
| 4   | Обе ветки `if`; отсутствующая `else` даёт `()`, не вызов  | Блок-ветка: её последний стейтмент                                              |
| 5   | `(e)` группировка                                         | Наследует хвостовость                                                           |
| 6   | Правый операнд `and`/`or`                                 | Его значение и есть результат. Текущий layout `CALL; JMP→RETURN` это уже делает |
| 7   | Тело каждой ветки `recv`, тело `else`, тело `after`       | `TestTailRecursionThroughRecv`, `TestSchedulerTailRecursionActor`               |

**Не хвостовые:**

- Любой не последний стейтмент.
- RHS `let`.
- Операнды бинарных/унарных операций, элементы литералов, аргументы вызовов.
- Условие `if`, таймаут `after`.
- **Всё тело `trap`, в том числе без `ensure`.**

Тело `trap` не хвостовое, потому что результат оборачивается `MAKEOK`, а обработчик живёт в этом кадре: замена кадра потеряла бы и то и другое. Старый `len(f.handlers)==0` давал ровно то же: внутри `trap` обработчик всегда есть. Принцип #11 в формулировке «TCO кроме активного `ensure`» выполняется как частный случай. «TCO сквозь `ensure`» остаётся **Should** (§16) и не реализуется.

Для `match`/`with`, когда они появятся (сегодня они вне компилятора, K-8), правило то же: ветки `match` и последний стейтмент/тело `with` наследуют `dest.tail`, тело `with … else` тоже.

**Сочетание с trap.** Инвариант компилятора: `TAILCALL` не эмитится при `trapDepth > 0`. `Verify` проверяет то же линейным проходом: `TRAPBEGIN` даёт `+1`, `TRAPEND` даёт `-1`, `TAILCALL` при глубине > 0 — ошибка. Регионы trap непрерывны и вложены, поэтому линейного прохода достаточно. VM дополнительно проверяет `len(f.handlers) == 0` и иначе завершает актор внутренней ошибкой.

```go
case OpTailCall:
 base, argc := i.A(), i.B()
 cv := regs[base]
 args := regs[base+1 : base+1+argc]
 if len(f.handlers) != 0 {
  return fail(errors.New("internal: TAILCALL under active trap"))
 }
 if cv.Kind == runtime.KindFunction && cv.Func != nil && cv.Func.IsNative {
  // хвостовой вызов native = вызвать и вернуть результат
  if ar := cv.Func.Arity; ar >= 0 && ar != argc {
   return fail(fmt.Errorf("(:function_clause, (%s, %d args))", cv.Func.Name, argc))
  }
  r, err := cv.Func.Native(s.vm, append([]runtime.Value(nil), args...))
  if err != nil {
   return fail(err) // handlers пусты: unwind сделает tryUnwindRaise
  }
  a.result = r
  return stepDone
 }
 c, err := resolveCallee(cv)
 if err == nil {
  err = checkArity(c, argc)
 }
 if err != nil {
  return fail(err)
 }
 nregs := c.chunk.NumRegs
 if nregs <= cap(f.regs) {
  regs = f.regs[:nregs]
  bindArgs(regs, c.chunk, args) // args ⊂ старого regs: memmove-безопасно
  clear(regs[c.chunk.NumParams:cap(regs)])
 } else {
  regs = make([]runtime.Value, nregs)
  bindArgs(regs, c.chunk, args)
 }
 f.regs, f.chunk, f.name, f.captures, f.ip = regs, c.chunk, c.name, c.captures, 0
 // f.callDst НЕ трогаем: результат уйдёт туда же, куда ушёл бы у исходного вызова
 return stepContinue
```

---

### 5. trap / ensure

**Инструкции.**

- `TRAPBEGIN A sBx` кладёт в стек обработчиков `trapHandler{ip: ip+1+sBx, errReg: A}`.
- `TRAPEND` снимает верхний обработчик.
- `MAKEOK A B` даёт `R[A] = Ok(R[B])`, `MAKEERROR A B` даёт `R[A] = Error(R[B])`.
- При `raise` в кадре VM снимает обработчик, записывает `regs[errReg] = val` и переходит на `ip`. Если `raise` пришёл из вызванного кадра, то же делает `tryUnwindRaise`.

**Схема без `ensure`.** Регистр ошибки и регистр результата совпадают с целевым `T`. Это безопасно: `T` на входе в `trap` мёртв.

```
      TRAPBEGIN  T  →H
      <inner → T>                 ; НЕ хвостовая позиция
      TRAPEND
      MAKEOK     T  T
      JMP        →END
H:    MAKEERROR  T  T
END:
```

**Схема с `ensure`.** `E` (ошибка) и `F` (флаг «была ошибка») — временные выше `T`. Флаг `Bool` заменяет прежнее сравнение с атомом `:no_error` (D-5). Внешняя пара `TRAPBEGIN/TRAPEND` из регистровой схемы не нужна: в её защищённом участке нет инструкций, способных бросить `*ErrRaise`.

```
      LOADK      F  #false
      TRAPBEGIN  E  →BH
      <body → T>                  ; область видимости, не хвост
      TRAPEND
      JMP        →ENS
BH:   LOADK      F  #true         ; E уже содержит ошибку тела
ENS:                              ; ensure — в порядке, обратном тексту (LIFO, §10.3)
   для k = last..first:
      TRAPBEGIN  E  →EH_k         ; ошибка ensure перезапишет E: побеждает последняя
      <ensure_k → S>              ; S — временный регистр
      TRAPEND
      JMP        →NEXT_k
EH_k: LOADK      F  #true
NEXT_k:
      JMPIF      F  →ERR
      MAKEOK     T  T
      JMP        →END
ERR:  MAKEERROR  T  E
END:
```

**LIFO и «побеждает последняя».** Ensure исполняются от последнего по тексту к первому. Каждый защищён собственным `TRAPBEGIN`, поэтому падение одного не мешает остальным. Записывать в `E` может только сработавший обработчик, значит побеждает **последняя по времени** ошибка. Это то же, что и раньше (`TestTrapEnsureLifo*`, `TestEnsureAllRunOnFailure`).

**Регистры зафиксированы.** Обработчик знает только `errReg`. Компилятор гарантирует, что на входе в код-обработчик живы лишь регистры, выделенные до `TRAPBEGIN`.

**Область активного `ensure`.** Это всё между первым `TRAPBEGIN` и последним `TRAPEND`, включая сами `ensure`. Все вызовы в ней — обычные `CALL` (§4).

---

### 6. Акторы

| Инструкция        | Формат | Семантика                                                                                  |
| ----------------- | ------ | ------------------------------------------------------------------------------------------ |
| `SPAWN A B C`     | ABC    | `R[A] = Pid(spawn(R[B]))`; `C == 1` — с `watch` со стороны родителя                        |
| `SEND A B C`      | ABC    | `R[A] = send(pid=R[B], msg=R[C])`, `Result<(), Atom>`; не-Pid — фатальная `type_error`     |
| `SELF A`          | A      | `R[A] = Pid(a.pid)`                                                                        |
| `MAKEREF A`       | A      | `R[A] = Ref(next)`                                                                         |
| `WATCH A B`       | AB     | `R[A] = Ref` наблюдения за `R[B]`                                                          |
| `UNWATCH A B`     | AB     | снять наблюдение `R[B]`, `R[A] = ()`                                                       |
| `MAILBOXSIZE A B` | AB     | `R[A] = len(mailbox(R[B]))` (мёртвый — 0)                                                  |
| `RECVTIMER A`     | A      | дедлайн `= now + R[A] ms`; `R[A]` обязан быть `Int` (иначе фатальная `type_error`)         |
| `RECVTAKE A sBx`  | AsBx   | см. ниже                                                                                   |
| `MATCHLOCAL A Bx` | ABx    | `R[A]` — сопоставляемое значение, `Bx` — индекс в `Chunk.Patterns`, fail — следующая `JMP` |
| `YIELD`           | —      | отдать квант (сохраняется без изменений)                                                   |

**`RECVTAKE`.** Реализует K-6:

1. Есть `downMsgs` — берём первое.
2. Иначе есть `mailbox` — берём первое; в обоих случаях дедлайн сбрасывается, сообщение идёт в `R[A]`, `ip++`.
3. Иначе, если дедлайн установлен и истёк: сбросить дедлайн и перейти на `ip+1+sBx` (`after`).
4. Иначе вернуть `stepBlock`, `ip` не меняется.

При отсутствии `after` `sBx = 0` и не читается: дедлайн не установлен, третья ветка недостижима. Отдельного «нет after»-маркера (раньше `0xFFFF`) не нужно.

**Структуры.** `Actor`, `Scheduler`, `Send/Watch/…` не меняются.

---

### 7. Регистровый аллокатор компилятора

```go
type scope struct {
 names map[string]int // имя → регистр
 mark  int            // nextReg на входе в область
}

type funcCompiler struct {
 compiler  *Compiler
 parent    *funcCompiler
 prefix    string
 chunk     *vm.Chunk
 nextReg   int // первый свободный регистр (bump)
 maxReg    int // high-water mark → chunk.NumRegs
 scopes    []scope
 bound     [vm.MaxRegs]bool // регистры именованных локалей (для инварианта I-3)
 localFns  map[string]string
 upvalues  []upvalueInfo
 consts    map[constKey]int
 trapDepth int
 pos       vm.SrcPos
}

type dest struct {
 reg  int  // регистр результата; в tail-контексте — scratch
 tail bool // результат — значение функции, управление не возвращается
}

func val(r int) dest { return dest{reg: r} }
```

**`freeRegs` не вводится.** `CALL`, `TUPLE/LIST/VECTOR/MAP` и `MAKECLOSURE` требуют **последовательных** регистров. Список свободных регистров фрагментировался бы, и нужен был бы поиск непрерывных окон. Стековая дисциплина (bump + `releaseToMark`) даёт последовательность бесплатно: значение `i`-го аргумента лежит ровно в `base+1+i`.

**Сигнатура: `compileExpr(e ast.Expr, d dest) error`.** Это сочетание двух вариантов: целевой регистр приходит параметром, а хвостовость едет вместе с ним. Обоснование:

- Вариант «вернуть индекс регистра» заставляет выражение само выбирать регистр, и для `let x = trap(...)` результат пришлось бы копировать `MOVE`.
- Чистый `target int` не даёт способа передать хвостовость: пришлось бы дублировать `compileIf`/`compileRecv` для tail и не-tail.
- Отдельного знания «где живёт значение» не хватает лишь для операндов, которые уже лежат в регистре: локальная переменная. Их обслуживает `operand()`.

```go
// operand: регистр со значением e. Локальная переменная — её собственный
// регистр, код не эмитится (безопасно: локали неизменяемы, K-1 сохраняется,
// т.к. значение не может измениться между вычислением и использованием).
func (fc *funcCompiler) operand(e ast.Expr) (int, error) {
 if v, ok := e.(ast.VariableExpr); ok {
  if r, ok := fc.resolveLocal(v.Name()); ok {
   return r, nil
  }
 }
 r := fc.allocReg()
 return r, fc.compileExpr(e, val(r))
}

// operandInto: то же, но «нелокальное» вычисляется прямо в into.
func (fc *funcCompiler) operandInto(e ast.Expr, into int) (int, error) {
 if v, ok := e.(ast.VariableExpr); ok {
  if r, ok := fc.resolveLocal(v.Name()); ok {
   return r, nil
  }
 }
 return into, fc.compileExpr(e, val(into))
}

// finish: значение выражения лежит в from.
func (fc *funcCompiler) finish(d dest, from int) {
 switch {
 case d.tail:
  fc.emit(vm.ABC(vm.OpReturn, from, 0, 0))
 case from != d.reg:
  fc.emit(vm.ABC(vm.OpMove, d.reg, from, 0))
 }
}
```

Операнды `operand()` и `operandInto()` нужны из-за размера `Value`: MOVE копирует ~290 байт, поэтому лишние копирования локалей — заметная стоимость.

**Временные регистры**

| Конструкция        | Схема                                                                                                                                                       |
| ------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Бинарная `l op r`  | `l' := operandInto(l, d.reg)`; `r' := operand(r)`; `OP d.reg l' r'`; `releaseToMark`                                                                        |
| Вызов              | `base := allocReg()` ← callee; аргумент `i`: `r := allocReg()` (`r == base+1+i`, инвариант I-2) ← значение; `CALL base argc d.reg` или `TAILCALL base argc` |
| Коллекция          | Элементы в последовательные регистры, `TUPLE/LIST/VECTOR d.reg base n`; для `MAP` пары `k,v` подряд                                                         |
| Замыкание          | Окно `1+len(upvalues)`: `LOADK base ⇐ функция`, затем `MOVE` (локаль родителя) или `GETUPVAL` (upvalue родителя) в `base+1+j`; `MAKECLOSURE d.reg base n`   |
| Переменная-upvalue | `GETUPVAL d.reg idx`                                                                                                                                        |

Каждый раз, когда конструкция использует временные регистры, она делает `mark := fc.nextReg` в начале и `releaseToMark(mark)` в конце.

**`releaseToMark(mark)`.**

- Освобождает всё, что выделено выше `mark`: временные и локали закрываемой области; для них сбрасывается `bound`.
- Не освобождает регистры ниже `mark`: параметры, регистры уже объявленных локалей, целевой регистр родительской конструкции (выделен до `mark`), алиас регистра сообщения в `recv`.
- `popScope()` = `releaseToMark(scope.mark)` + снятие имён.

**Локали.** `let x = e` выделяет регистр **до** компиляции RHS (`r := allocReg()`), компилирует RHS в `r`, затем `bindLocal("x", r)`. Так RHS видит внешний `x` (затенение, §6.6), а регистр остаётся занятым до конца области.

**Инварианты (проверяются всегда, стоимость пренебрежимо мала)**

- **I-1.** `compileExpr` не меняет `nextReg` (стек-нейтральность).
- **I-2.** Аргумент `i` вызова лежит в `base+1+i`.
- **I-3.** После `bindLocal(r)` ни одна инструкция не пишет в `r` явным операндом `A`. Первичное определение происходит до привязки; `MATCHLOCAL` пишет неявно и разрешён.
- **I-4.** Любой регистр-операнд в `emit` строго меньше `nextReg`.

`vm.RegUse(i Instr) (reads, writes []int)` — общая таблица чтения/записи регистров для `emit` и `Verify`. Проверка I-4 в `emit` ловит «регистр освобождён раньше, чем прочитан»; I-3 ловит «регистр переменной затёрт временным»; I-1 и I-2 ловят «окно вызова затёрто вложенным вычислением».

**Переполнение 256.** `allocReg` вызывает `fc.fail(...)`, то есть `panic(compileError{...})`. `compileFunction` через `recover` превращает его в `error`, который возвращает `Compile`. CLI печатает `compile: function "f" needs more than 256 registers` с кодом выхода 3, а не Go-паникой.

**Spill не делаем.**

- Spill требует пары инструкций с адресом в памяти и учёта в каждом пути аллокатора: удвоение ISA и `Verify`.
- Лимит на живые регистры, а не на все объявленные, сам по себе ослабляет старый.
- Реальный источник переполнения — утечка аллокатора (`nextReg` не возвращается), и её надо чинить, а не маскировать.

**Локальные функции и upvalue.**

```go
func (fc *funcCompiler) resolveUpvalue(name string) (int, bool) {
 for i, uv := range fc.upvalues {
  if uv.name == name {
   return i, true
  }
 }
 p := fc.parent
 if p == nil {
  return 0, false
 }
 if r, ok := p.resolveLocal(name); ok {
  return fc.addUpvalue(name, true, r), true
 }
 if i, ok := p.resolveUpvalue(name); ok {
  return fc.addUpvalue(name, false, i), true
 }
 return 0, false
}
```

Порядок разрешения имени: локаль → локальная `fn` (собственная и предков → `GETGLOBAL outer$name`) → upvalue → глобал.

**`compileIf`, `compileTrap` (без ensure) и `compileRecv`** — основные образцы для остальных форм. Позиции (`fc.pos`) выставляются перед эмиссией собственной инструкции узла, после компиляции детей, чтобы `ADD` нёс позицию `+`, а не позицию последнего операнда.

```go
func (fc *funcCompiler) compileIf(ie ast.IfExpr, d dest) error {
 pos := posOf(ie)
 mark := fc.nextReg
 c, err := fc.operand(ie.Cond())
 if err != nil {
  return err
 }
 fc.pos = pos
 jElse := fc.emitJump(vm.OpJmpIfNot, c)
 fc.releaseToMark(mark) // регистр условия мёртв после проверки
 if err := fc.compileBranch(ie.ThenBody(), d); err != nil {
  return err
 }
 jEnd := -1
 if !d.tail {
  jEnd = fc.emitJump(vm.OpJmp, 0)
 }
 fc.patchHere(jElse)
 if eb := ie.ElseBody(); eb != nil {
  if err := fc.compileBranch(eb, d); err != nil {
   return err
  }
 } else if err := fc.loadUnit(d); err != nil { // отсутствующая else → ()
  return err
 }
 if jEnd >= 0 {
  fc.patchHere(jEnd)
 }
 return nil
}

func (fc *funcCompiler) compileInlineTrap(inner ast.Expr, d dest) error {
 fc.trapDepth++
 begin := fc.emitJump(vm.OpTrapBegin, d.reg) // errReg == T
 if err := fc.compileExpr(inner, val(d.reg)); err != nil {
  return err
 }
 fc.emit(vm.ABC(vm.OpTrapEnd, 0, 0, 0))
 fc.trapDepth--
 fc.emit(vm.ABC(vm.OpMakeOk, d.reg, d.reg, 0))
 jEnd := fc.emitJump(vm.OpJmp, 0)
 fc.patchHere(begin)
 fc.emit(vm.ABC(vm.OpMakeError, d.reg, d.reg, 0))
 fc.patchHere(jEnd)
 if d.tail {
  fc.emit(vm.ABC(vm.OpReturn, d.reg, 0, 0))
 }
 return nil
}
```

Блочная форма с `ensure` строится ровно по схеме §5. Для `recv` схема такая:

```
      [RECVTIMER t]                ; t ⇐ выражение after, вычисляется ДО RECVTAKE
      RECVTAKE  M →AFTER           ; M = msgReg, выделен первым
  для каждой ветки (в своей области; регистры паттерна выделены заранее):
      MATCHLOCAL M pat_i
      JMP →NEXT_i
      <body_i → d>                 ; хвост: сам заканчивается RETURN/TAILCALL
      [JMP →END]                   ; только не-хвост
NEXT_i:
  else:   алиас имя→M; <else body → d>; [JMP →END]
  иначе:  LOADK w0 #:recv_clause; MOVE w1 M; TUPLE w0 w0 2; RAISE w0
AFTER:    <after body → d>         ; только если есть after
END:
```

---

### 8. Константный пул и пул паттернов

```go
type SrcPos struct{ Line, Col int32 }

type Chunk struct {
 Code      []Instr
 Constants []runtime.Value
 Patterns  []*CompiledPattern // сохраняется
 Pos       []SrcPos           // параллельно Code
 NumRegs   int
 NumParams int
 Variadic  bool
}

func (c *Chunk) Emit(i Instr, pos SrcPos) int            // возвращает индекс инструкции
func (c *Chunk) PatchJump(at, target int) error          // сохраняет A/op, пишет sBx = target-(at+1)
func (c *Chunk) AddConstant(v runtime.Value) int
func (c *Chunk) AddPattern(p *CompiledPattern) int
func (c *Chunk) LineAt(ip int) int                       // совместимость
func (c *Chunk) PosAt(ip int) SrcPos
func (c *Chunk) Disassemble(name string) string
```

- **Адресация констант:** `LOADK A Bx`, `GETGLOBAL A Bx`, `SETGLOBAL A Bx` (16 бит, до 65 536). Имена глобалов — `Str`-константы.
- **Переполнение:** ошибка компиляции (§1). Обхода нет.
- **Дедупликация в компиляторе** (`fc.konst(v) int`) для скаляров. Функциональные значения, `Decimal`, `Bytes` не дедуплицируются.

```go
type constKey struct {
 kind runtime.Kind
 i    int64  // Bool (0/1) и small Int
 f    uint64 // math.Float64bits
 s    string // Str, Atom
}
```

- **Паттерны.** `Chunk.Patterns` сохраняется, адресация по `Bx`. Дедупликации нет: паттерн содержит номера регистров-связываний, разделять его нельзя. Поле `CompiledPattern.FailAddr` **удаляется**, `MatchPattern(v, p, regs)` пишет связывания в `regs` (параметр раньше назывался `locals`).
- **Порядок и стабильность индексов.** Константы нумеруются по первому использованию при детерминированном обходе AST: стейтменты по порядку, операнды слева направо. Функциональные константы добавляются после компиляции дочерней функции. Паттерны — по порядку появления. Это и есть контракт для bytecode-golden (§9, §12).

---

### 9. Дизассемблер

`Chunk.Disassemble(name string) string` — сигнатура сохраняется; `cmd/brig` вызывает её как раньше.

- **Заголовок:** `== <имя> arity=<Function.Arity> params=<NumParams>[ variadic] regs=<NumRegs> consts=<len(Constants)> patterns=<len(Patterns)> ==`.
- **Строка:** `%04d %3d:%-3d %-11s <операнды>`. Первое поле — индекс инструкции. Второе — `line:col`. Регистры печатаются как `rN`, константы как `kN`, паттерны как `pN`.
- **Декодирование 4-байтных инструкций** идёт по таблице формата:

| Формат | Вывод                                                                                                                         |
| ------ | ----------------------------------------------------------------------------------------------------------------------------- |
| ABC    | `A B C`; для `CALL` — `rA argc -> rC`; для `TUPLE/LIST/VECTOR/MAP` — `rA <- rB..rB+C-1`                                       |
| ABx    | `rA kBx ; <Inspect константы>` или `rA pBx ; <паттерн>`                                                                       |
| AsBx   | `rA -> %04d` (абсолютная цель); для `TRAPBEGIN` — `handler -> %04d`; для `RECVTAKE` — `after -> %04d`, только если `sBx != 0` |

- **Паттерны** печатаются встроенным комментарием в строке `MATCHLOCAL` (через `FormatCompiledPattern`, связывания как `rN` вместо `$N`). Отдельный блок `patterns:` не выводится.
- **Сохранение `line:col` (Must §15.1).** `Chunk.Pos` — по записи на инструкцию. Компилятор берёт `(line, col)` из `(Pos(), End())` узла, как `sema.posOf` (в текущем AST `Pos()` возвращает строку, `End()` — колонку).
- **Порядок функций.** Итерация по `map` в `cmd/brig` даёт случайный порядок; для детерминированного вывода (и для goldens) там нужно сортировать имена. Это единственная правка вне `ВХОДИТ` — три строки в `cmd/brig/main.go`, `sort.Strings`.

Пример: `fn fib(n) -> if n < 2 then n else fib(n - 1) + fib(n - 2)` (значения `line:col` иллюстративны).

```
== fib arity=1 params=1 regs=6 consts=3 patterns=0 ==
0000   2:12  LOADK       r3 k0 ; 2
0001   2:10  LT          r2 r0 r3
0002   2:5   JMPIFNOT    r2 -> 0004
0003   2:19  RETURN      r0
0004   2:26  GETGLOBAL   r2 k1 ; fib
0005   2:34  LOADK       r4 k2 ; 1
0006   2:32  SUB         r3 r0 r4
0007   2:26  CALL        r2 1 -> r1
0008   2:39  GETGLOBAL   r3 k1 ; fib
0009   2:47  LOADK       r5 k0 ; 2
0010   2:45  SUB         r4 r0 r5
0011   2:39  CALL        r3 1 -> r2
0012   2:37  ADD         r1 r1 r2
0013   2:37  RETURN      r1
```

---

### 10. Соответствие прежним стековым (удалённым) опкодам

Обозначения: `R[x]` — регистр, `K[x]` — константа, форматы — из §1.

| Старый опкод                                                     | Новый                  | Формат | Семантика                                                                                  |
| ---------------------------------------------------------------- | ---------------------- | ------ | ------------------------------------------------------------------------------------------ |
| `OpConstant`                                                     | `LOADK`                | ABx    | `R[A] = K[Bx]`                                                                             |
| `OpPop`, `OpDup`                                                 | **удалены**            | —      | Операнд-стека нет; отброшенное значение — временный регистр, освобождаемый `releaseToMark` |
| —                                                                | **`MOVE`** (новый)     | AB     | `R[A] = R[B]`                                                                              |
| `OpGetLocal`, `OpSetLocal`                                       | **удалены**            | —      | Локали — регистры; чтение — операнд, копирование — `MOVE`                                  |
| `OpGetGlobal`                                                    | `GETGLOBAL`            | ABx    | `R[A] = G[K[Bx].Str]`, иначе фатальная `undefined: name`                                   |
| `OpSetGlobal`                                                    | `SETGLOBAL`            | ABx    | `G[K[Bx].Str] = R[A]` (компилятор не эмитит, VM сохраняет)                                 |
| `OpGetUpvalue`                                                   | `GETUPVAL`             | AB     | `R[A] = captures[B]`                                                                       |
| `OpSetUpvalue`                                                   | **удалён**             | —      | Захваты — снимок по значению (K-7); запись в общий слайс `captures` нарушала бы §0 #13     |
| `OpAdd`, `OpSub`, `OpMul`, `OpDiv`, `OpIntDiv`, `OpRem`, `OpPow` | те же                  | ABC    | `R[A] = R[B] op R[C]`                                                                      |
| `OpEq`, `OpNeq`, `OpLt`, `OpGt`, `OpLe`, `OpGe`                  | те же                  | ABC    | `R[A] = Bool(R[B] cmp R[C])`                                                               |
| `OpNeg`, `OpNot`                                                 | `NEG`, `NOT`           | AB     | `R[A] = op R[B]`                                                                           |
| `OpJump`                                                         | `JMP`                  | sBx    | `ip += 1 + sBx`                                                                            |
| `OpJumpFalse`                                                    | **`JMPIFNOT`**         | AsBx   | Переход, если `R[A] == Bool(false)` (K-2)                                                  |
| `OpJumpTrue`                                                     | **`JMPIF`**            | AsBx   | Переход, если `R[A] == Bool(true)` (K-2)                                                   |
| `OpCall`                                                         | `CALL`                 | ABC    | `R[C] = R[A](R[A+1..A+B])`                                                                 |
| —                                                                | **`TAILCALL`** (новый) | AB     | Замена кадра вызовом `R[A](R[A+1..A+B])`                                                   |
| `OpReturn`                                                       | `RETURN`               | A      | Вернуть `R[A]`                                                                             |
| `OpTuple`, `OpList`, `OpVector`                                  | те же                  | ABC    | `R[A] = ctor(R[B..B+C-1])`                                                                 |
| `OpMap`                                                          | `MAP`                  | ABC    | `R[A] = map` из `C` пар в `R[B..B+2C-1]`                                                   |
| `OpRange`                                                        | `RANGE`                | ABC    | `R[A] = R[B] to R[C]`                                                                      |
| `OpIndex`                                                        | `INDEX`                | ABC    | `R[A] = R[B][R[C]]`                                                                        |
| `OpRaise`                                                        | `RAISE`                | A      | `raise R[A]`                                                                               |
| `OpMakeClosure`                                                  | `MAKECLOSURE`          | ABC    | `R[A] = Closure(R[B], R[B+1..B+C])`                                                        |
| `OpTrapBegin`                                                    | `TRAPBEGIN`            | AsBx   | Обработчик `{ip+1+sBx, errReg=A}`                                                          |
| `OpTrapEnd`                                                      | `TRAPEND`              | —      | Снять обработчик                                                                           |
| `OpMakeOk`, `OpMakeError`                                        | `MAKEOK`, `MAKEERROR`  | AB     | `R[A] = Ok/Error(R[B])`                                                                    |
| `OpSpawn`                                                        | `SPAWN`                | ABC    | `R[A] = spawn(R[B])`, `C` — linked                                                         |
| `OpSend`                                                         | `SEND`                 | ABC    | `R[A] = send(R[B], R[C])`                                                                  |
| `OpSelf`, `OpMakeRef`                                            | `SELF`, `MAKEREF`      | A      | `R[A] = …`                                                                                 |
| `OpWatch`, `OpUnwatch`, `OpMailboxSize`                          | те же                  | AB     | `R[A] = op(R[B])`                                                                          |
| `OpRecvTimer`                                                    | `RECVTIMER`            | A      | Дедлайн из `R[A]`                                                                          |
| `OpRecvTake`                                                     | `RECVTAKE`             | AsBx   | `R[A]` = сообщение; `sBx` → `after`                                                        |
| `OpMatchLocal`                                                   | `MATCHLOCAL`           | ABx    | Матч `R[A]` с `Patterns[Bx]`; следующая `JMP` — fail                                       |
| `OpYield`                                                        | `YIELD`                | —      | Отдать квант                                                                               |

Итого 49 опкодов: удалено 5 (`Pop`, `Dup`, `GetLocal`, `SetLocal`, `SetUpvalue`), добавлено 2 (`MOVE`, `TAILCALL`).

---

### 11. План миграции

Работа идёт в ветке `sprint7-regvm`. Перед стартом `make all` зелёный, ставится тег `stack-vm-final`, и с него собирается «старый» бинарник для дифференциального прогона.

| Подэтап | Файлы                                                                                                                                                                                                                                                                                               | Собирается?                                                          |
| ------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------- |
| S7.1    | `opcodes.go`, `chunk.go` (с дизассемблером), `pattern.go` (−`FailAddr`, `$N`→`rN`), `verify.go`, `regs_test.go` (encode/decode, `PatchJump`)                                                                                                                                                        | **Нет**: `scheduler.go` ещё на старом ISA. Коммит `wip:`             |
| S7.2    | `compiler.go` целиком                                                                                                                                                                                                                                                                               | **Нет**: пока не собирается `vm`. Коммит `wip:`                      |
| S7.3    | `scheduler.go`: `Frame`, `trapHandler`, `frameFromFn`, ядро `stepFrame` (`LOADK`, `MOVE`, арифметика/сравнения, `JMP*`, `CALL/TAILCALL/RETURN`, `GETGLOBAL`, `TRAP*`, `MAKEOK/ERROR`, `TUPLE/LIST/VECTOR/MAP/RANGE/INDEX`, `MAKECLOSURE`, `GETUPVAL`); остальные опкоды — `fail("not implemented")` | **Да** — первый собираемый коммит. Гоняются `TestHello`, `TestArith` |
| S7.4    | Акторные опкоды: `SPAWN/SEND/SELF/MAKEREF/WATCH/UNWATCH/MAILBOXSIZE/RECVTIMER/RECVTAKE/MATCHLOCAL/YIELD`; правка `runSlice`/`callSync`/`tryUnwindRaise`                                                                                                                                             | Да                                                                   |
| S7.5    | Дизассемблер до финала, `cmd/brig` (sort), bytecode-goldens                                                                                                                                                                                                                                         | Да                                                                   |
| S7.6    | Полный прогон, дифференциальный, race, D-тесты                                                                                                                                                                                                                                                      | Да                                                                   |
| S7.7    | Документация (§13)                                                                                                                                                                                                                                                                                  | Да                                                                   |

**Что ломается раньше.** `vm` ломается первым (S7.1, потому что `scheduler.go` ссылается на удалённые константы). Компилятор — сразу следом (S7.1: `Emit(op, operand, line)` и `EmitTwo` исчезают). Дерево не собирается на промежутке S7.1..S7.2; коммиты на нём помечаются `wip:`, pre-push хук на ветке отключён, в `main` идёт squash после S7.6.

**Порядок отладки первого зелёного прогона:**

1. `go vet ./internal/vm`, затем `go test ./internal/vm -run TestArithmetic|TestPreludePrint`.
2. `TestHello`, `TestArith` — константы, `MOVE`, `CALL` native, `RETURN`.
3. `TestRecursion`, `TestAndOr` — `JMPIFNOT`, K-2, вызовы байткод-функций.
4. `TestLocalFn`, `TestClosure*`, `TestMutualRecursion` — окна замыканий, `GETGLOBAL` мангленных имён.
5. Все `TestTrap*` — схемы из §5, D-5.
6. `TestTailRecursion*`, `TestSchedulerTailRecursionActor` — TCO (диагностика — §12).
7. Акторные тесты (`TestSpawn*`, `TestRecv*`, `TestScheduler*`).
8. Prelude/Json/Test-framework/Decimal/Bytes.
9. `make run-examples`, `make test-race`, `make all`.
10. Дифференциальный прогон: для каждого `examples/*.brig` `diff <(old-brig run f) <(new-brig run f)` (вывод должен совпасть до байта).

**Отладочный инструмент.** Файл `trace.go` под тегом `//go:build brigtrace`: `stepFrame` печатает `name:ip op` и дельту регистров. Без тега — константа `traceEnabled = false`, компилятор Go выбрасывает код. Оболочка `recover` в `runSlice`, включаемая тем же тегом или `BRIG_VERIFY=1`, превращает Go-панику индекса в `internal: <fn>@<ip> <op>`.

**Новые тесты** (не меняют существующие): `regvm_test.go` (D-1..D-5, `TestTailCallEmitted`, `TestTailCallArgOrder`, `TestPatchJump`), `verify_on_test.go` (`func init(){ compiler.Verify = true }`), `testdata/bytecode/*.txt` (goldens).

---

### 12. Риски

**Ожидаемые баги аллокатора и как они ловятся**

| Баг                                                       | Проявление                                        | Ловится                                                                                 |
| --------------------------------------------------------- | ------------------------------------------------- | --------------------------------------------------------------------------------------- |
| Временный регистр освобождён до потребителя               | Инструкция читает регистр ≥ `nextReg`             | I-4 в `emit`                                                                            |
| Регистр callee/окна затёрт                                | Вложенное вычисление получило регистр внутри окна | I-1 (стек-нейтральность), I-2 (`r == base+1+i`)                                         |
| Регистр переменной переиспользован в следующем стейтменте | Значение локали затёрто временным                 | I-3 (запись в `bound` регистр) и `Verify`                                               |
| Целевой регистр «доедается» между стейтментами            | Результат стейтмента N портится в N+1             | I-1; для `let` — `bindLocal` требует `reg < nextReg`                                    |
| Слепая запись мимо `NumRegs`                              | Go-panic индекса                                  | `Verify` (все регистры и окна < `NumRegs`)                                              |
| `TAILCALL` затирает аргументы при перекрытии              | Неверные значения при перестановке аргументов     | `TestTailCallArgOrder` (`go(n-1, acc+n)` и с перестановкой); `memmove`-семантика `copy` |
| Устаревшие значения после переиспользования `regs`        | Чтение мусора от предыдущей функции               | `clear(regs[NumParams:cap])`; `Verify` (definite assignment)                            |
| `TAILCALL` внутри trap                                    | Потеря `ensure`                                   | Инвариант компилятора + `Verify` + проверка в VM                                        |
| Native хранит слайс аргументов                            | Порча коллекций                                   | K-5, `TestListLiteralViaNative` в `regvm_test.go`                                       |

**`Verify` (`vm/verify.go`).** Включается флагом `compiler.Verify` (в тестах) или `BRIG_VERIFY=1`. Проверяет:

- регистры и окна в `< NumRegs`;
- цели переходов внутри кода;
- за каждым `MATCHLOCAL` идёт `JMP`;
- `TAILCALL` вне регионов trap (линейный счётчик глубины);
- нет «падения с конца» кода;
- definite assignment: прямой анализ «регистр определён на всех путях». Параметры определены на входе. Обработчик `TRAPBEGIN` наследует состояние на `TRAPBEGIN`, плюс `errReg`. `MATCHLOCAL` определяет регистры паттерна на успешном ребре.

**Bytecode-goldens.** `testdata/bytecode/{hello,arith,fib,trap_ensure,recv_after,closure,tail}.txt` сверяются с `Disassemble` (флаг `-update` как у parser-goldens). Стабильность обеспечена детерминированным порядком констант (§8).

**Функция превысила 256 регистров.**

1. Сначала предполагаем утечку аллокатора: прогнать с `BRIG_VERIFY=1`, найти конструкцию, после которой `nextReg` не возвращается.
2. Если это настоящая программа, то единственный выход — разбить функцию. Автоматического spill не будет.
3. Если причина — литерал: при `> ~250` элементов список можно строить порциями (`LIST` + `ADD`, конкатенация уже есть в `add`); для `TUPLE/VECTOR/MAP` понадобился бы `APPEND A B C`, но пока этот случай не встречался, опкод не вводится.

**Если `TestTailRecursion` (10⁶ вызовов) не проходит**, идём по порядку:

1. `brig run --dump-bytecode` на `sum_to`: есть ли `TAILCALL`? Если нет, значит `dest.tail` потерялась в `compileIf`/`compileBranch`/`compileBlock` — исправляем компилятор. Возвращать эвристику `isTailCall` **нельзя**.
2. Не открыт ли `trapDepth`/обработчик (`len(f.handlers)`).
3. Растёт ли `len(a.frames)`: если да, `TAILCALL` не отработал.
4. Правильность порядка `bindArgs` при перекрытии, если результат неверный.
5. Время: замерить `go test -run TestTailRecursion -v`, ожидаем секунды, не минуты. Если долго — смотреть аллокации `make` при `NumRegs > cap` и профиль `Value`-копирования; если аллокации доминируют, включается пул `regs` (открытый вопрос 1).

**Унаследованные ограничения** (не решаются дизайном, чтобы не смешивать с миграцией): K-8; пробелы вне K-8 — см. A-F8 рядом с K-8 выше.

---

### 13. Снятие отступления от §15.1

Решение по историческому блоку: **удалить**. Шапка `architecture.md` описывает текущее состояние. Утверждение «мы отступаем от §15.1» после Sprint 7 ложно. История сохраняется в git (тег `stack-vm-final`) и в `CHANGELOG.md`. В шапке вместо блока остаётся одна строка с указанием на раздел «Register VM design».

Diff шапки `docs/architecture.md`:

```diff
 Референсная реализация — **на Go** (решение v0.3.0, §13). Дизайн-инварианты:
 иммутабельные значения (#13), TCO вне активного `ensure` (#11), общий heap,
 один бинарник.

-> **Осознанное отступление от §15.1.** Спецификация требует регистровую VM
-> (BEAM/Lua-style). Текущая реализация — **регистровая**: вертикальный срез для
-> быстрой стабилизации семантики. Миграция с прежней стековой схемой на регистровую с полноценным
-> дизассемблером — отдельный спринт, см. `STATUS.md`.
+> **VM — регистровая (§15.1).** 4-байтные инструкции `[op|A|B|C]`, до 256
+> регистров на кадр, явный `TAILCALL`, дизассемблер `--dump-bytecode` с
+> `line:col`. Устройство — в разделе «Register VM design» ниже.
```

Остальные правки в `architecture.md`:

- В mermaid «Слои»: подпись `internal/vm` — «регистровая ВМ + scheduler» (обновлено).
- Разделы «TCO и `ensure` (§15.3)» и «trap / ensure — схема байткода» заменяются разделом «Register VM design» (подразделы 4 и 5).
- В «Статус реализации»: `- [ ] **Регистровая VM**…` → `- [x]`; строку «Этап 4 (часть): регистровая VM…» обновить (это завершённый этап).

Правки вне `architecture.md`:

- `README.md`: обновить пункт (см. README.md); в структуре `vm/ # стековая ВМ` → `регистровая ВМ`.
- `STATUS.md`: закрыть чеклист Sprint 7; удалить процитированную в нём «Осознанное отступление от §15.1…».
- `cmd/brig/main.go`, `internal/vm/vm.go`, `internal/vm/opcodes.go`, `internal/compiler/compiler.go`: в комментариях-заголовках «стековая» → «регистровая» (выполнено).

---

## Оценка по подэтапам

| Подэтап                                                                                         | Дни                                                    |
| ----------------------------------------------------------------------------------------------- | ------------------------------------------------------ |
| S7.0 Подготовка: тег, ветка, baseline `make all`, снимок вывода `examples/*.brig`               | 0.5                                                    |
| S7.1 `opcodes.go`, `chunk.go`, `pattern.go`, `verify.go`, unit-тесты кодирования                | 1.5                                                    |
| S7.2 `compiler.go`: аллокатор, `compileExpr`/`Call`/`If`/`Trap`/`Recv`/`Lambda`, паттерны, REPL | 3                                                      |
| S7.3 `scheduler.go`: кадр, `frameFromFn`, ядро `stepFrame`, `TAILCALL`                          | 2                                                      |
| S7.4 Акторные опкоды и правки `runSlice`/`callSync`/`tryUnwindRaise`                            | 1                                                      |
| S7.5 Дизассемблер, sort в `cmd/brig`, bytecode-goldens                                          | 0.5                                                    |
| S7.6 Зелёный прогон, дифференциальный, race, D-тесты                                            | 2                                                      |
| S7.7 Документация (§13)                                                                         | 0.5                                                    |
| **Итого**                                                                                       | **≈ 11** (верхняя граница «1–2 недели» из `STATUS.md`) |

## Открытые вопросы

Решаются в коде, по измерениям:

1. **Пул `regs` в `Actor`** (free-list по размеру кадра). По умолчанию не включать. Решить по профилю `TestTailRecursion` и `fib(27)` после S7.6.
2. **`Verify` (dataflow-проход) в `brig run` по умолчанию.** По умолчанию только тесты и `BRIG_VERIFY=1`. Решить по замеру стоимости на `examples/*.brig`.
3. **Аудит `Arity` native в прелюдии.** D-2 превратит неверно объявленную арность из молчаливого дефекта в ошибку. Выяснится при прогоне S7.6.
