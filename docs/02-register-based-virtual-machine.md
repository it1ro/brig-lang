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
| TCO          | явный `TAILCALL A B`, который эмитит компилятор; `isTailCall` удаляется; сквозь `ensure` — §5.1     |
| trap         | `TRAPBEGIN A sBx`: `A` — регистр, куда падает ошибка; `stackLen` удаляется                          |
| Паттерны     | `MATCHLOCAL A Bx` + обязательный следующий `JMP` (fail)                                             |
| Аллокатор    | bump-указатель со стековой дисциплиной (`nextReg`, `releaseToMark`); `freeRegs` и spill не вводятся |
| Escape-форма | нет                                                                                                 |

### Контракты, которые дизайн сохраняет (фиксируются явно)

Часть этих поведений в регистровой VM «случайна». Они наблюдаемы, поэтому становятся контрактом.

- **K-1. Порядок вычисления.** Callee вычисляется раньше аргументов. Аргументы, операнды и элементы литералов вычисляются слева направо. В `%{}` для каждой пары сначала ключ, затем значение. `and`/`or` — короткое замыкание.
- **K-2. Условные переходы (строгий `Bool`, DD #41 = A).** Условие `if` и **оба** операнда `and`/`or` обязаны быть `Bool` (§7.2). `JMPIFNOT` прыгает на `Bool(false)`, `JMPIF` — на `Bool(true)`; на любом не-`Bool` они бросают ловимый `(:type_error, (:expected_bool, v))`. Truthiness нет. `and`/`or` всегда возвращают `Bool`. Правый операнд `and`/`or` проверяется после вычисления, поэтому он **не** в хвостовой позиции (§4, T-82 — won't-fix); хвостовая форма — `if c then true else loop(n - 1)`.
- **K-3. Что ловит `trap` (DD #42 = A).** Все `:type_error` ловимы: арифметика (включая унарный минус), `not` на не-`Bool`, `runtime.Compare`, не-`Bool` в условии (K-2), вызов не-функции, нативы прелюдии, `INDEX`/range/поле, акторные примитивы, сравнение `Decimal` с `Float` (раздел «Сравнение чисел» ниже); также ловим `:function_clause` при несовпадении арности (T-96). Каждый — `*ErrRaise` и попадает в `trap` так же, как `raise`. Фатальны для актора только внутренние инварианты VM (сообщения с префиксом `internal:`, exit 3 в CLI). §10.4 спеки не меняется: `:type_error` там уже перечислен среди авто-raise.
- **K-4. Редукции.** Одна редукция — это `CALL` в байткод-функцию, `TAILCALL` в байткод-функцию, `RETURN` и шаг unwind. Вызов native редукций не тратит.
- **K-5. Аргументы native.** Native получает свежий слайс (`runtime.List(args...)`, `Variant("Some", args...)` его удерживают). Слайс на окно регистров передавать нельзя.
- **K-6. recv.** Сначала `downMsgs`, потом `mailbox`. Дедлайн живёт в `Actor.recvDeadline` и сбрасывается при взятии сообщения. Пока актор заблокирован, `ip` стоит на `RECVTAKE`.
- **K-7. Замыкания.** Лямбда всегда даёт `KindClosure` (даже с 0 захватов), захваты — снимок по значению (N12, `TestClosureCapture`). Локальная `fn` остаётся глобальной `Function` с именем `outer$name`.
- **K-8 и A-F8 (закрыты).** Конструкции, которых не было на момент миграции (`match`, `with`, вариадики, pipe `|>`, record-литералы, `link`, `Sys.args()`, `mailbox_size()` без аргументов), реализованы в Wave 6: T-70…T-78.

### Сознательные исправления (единственные отклонения от регистрового поведения)

Все пять — латентные баги, которых не касается ни один существующий тест. Каждый получает новый тест в `internal/compiler/regvm_test.go`.

| №   | Было                                                                                                                | Стало                                                                                     | Обоснование                                                        |
| --- | ------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| D-1 | Variadic `fn f(x, ..rest)`: `frameFromFn` копирует аргументы в слоты как есть, `rest` — «сырой» второй аргумент     | `rest` = `List` остальных аргументов                                                      | §6.3; компилятор уже помечает `Arity == -1`                        |
| D-2 | Native с неверным числом аргументов → Go-panic (`args[0]`)                                                          | Ловимый raise `(:function_clause, args)` (K-3, T-96)                                | Panic → диагностируемая ошибка                                     |
| D-3 | `recv … else msg`: имя `msg` не связывается (`undefined: msg`)                                                      | `msg` — алиас регистра сообщения                                                          | §12.4                                                              |
| D-4 | Одна плоская область на функцию (переменные блока видны после блока); upvalue только на 1 уровень вложенности       | Настоящие лексические области; upvalue рекурсивно (`upvalueInfo.isLocal` уже так задуман) | §6.6; повторное использование регистров требует настоящих областей |
| D-5 | `trap` с `ensure` различает успех и ошибку сравнением с атомом `:no_error`, поэтому `raise(:no_error)` даёт `Ok(…)` | Флаг-регистр `Bool`                                                                       | §10.3                                                              |

---

### Сравнение чисел `Int`/`Float`/`Decimal` (DD #43: 1 = A, 2 = A, 3 = A)

Нормативно (§4.8, §7.4 спеки); три места, где числа разных видов встречаются, определены по отдельности.

1. **Паттерны с числовым литералом — строго по виду.** Сопоставление требует того же `Kind` и точного значения (`runtime.MatchEqual`, также внутри контейнеров). Паттерн `1` не матчит `1.0` и `dec"1"`; `1.0` не матчит `dec"1.0"`.
2. **`Int × Float` — точное сравнение (T-85).** Без промежуточного `float64`: `2^53 + 1 == 2^53.0` ложно, равенство транзитивно, `1 == 1.0` истинно. `NaN != NaN`, `NaN` не меньше и не больше никого; `+Inf` больше любого `Int`, `-Inf` меньше.
3. **`Decimal × Float` в операторах.** `==`/`!=` — ловимый `(:type_error, (:eq, (a, b)))` (`checkMixedEq`); `<`, `>`, `<=`, `>=` — ловимый `(:type_error, (:compare, (a, b)))` (`checkMixedCmp`, `runtime.Compare`). `==`/`!=` проверяют только пару верхнего уровня; `Decimal` и `Float`, вложенные в контейнеры под `==`, дают `false`, без ошибки. `Decimal × Int` и `Decimal × Decimal` сравниваются по значению точно.
4. **Ключи `Map`/`Set` (`INDEX`, `set`, `Map.*`) — `runtime.KeyEqual`.** Как `==`, но `Decimal` равен только `Decimal` (в т.ч. внутри контейнеров): `dec"1"` не равен ни `1`, ни `1.0`, ошибки нет. `Int × Float` остаётся численным (`1` и `1.0` — один ключ), поэтому дедупликация транзитивна.

Пример: `trap(dec"1" < 1.0)` даёт `Error((:type_error, …))`, а `match 1.0` с веткой `dec"1.0"` не срабатывает и не бросает ошибку. `trap(1 < "a")` — не type error: это валидное сравнение по term order (§7.4), результат `true`.

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
| 6   | ~~Правый операнд `and`/`or`~~ — **не хвостовая** (K-2)    | Результат проверяется на `Bool` после вычисления. Хвостовая форма — `if c then true else f(..)` |
| 7   | Тело каждой ветки `recv`, тело `else`, тело `after`       | `TestTailRecursionThroughRecv`, `TestSchedulerTailRecursionActor`               |

**Не хвостовые:**

- Любой не последний стейтмент.
- RHS `let`.
- Операнды бинарных/унарных операций, элементы литералов, аргументы вызовов.
- Условие `if`, таймаут `after`.
- Оба операнда `and`/`or` (правый — тоже, K-2).
- **Всё тело `trap`, в том числе без `ensure`.** Единственное исключение — хвост тела `trap` с `ensure` по схеме §5.1.

Тело `trap` не хвостовое, потому что результат оборачивается `MAKEOK`, а обработчик живёт в этом кадре: замена кадра потеряла бы и то и другое. Старый `len(f.handlers)==0` давал ровно то же: внутри `trap` обработчик всегда есть. Принцип #11 в формулировке «TCO кроме активного `ensure`» выполняется как частный случай. «TCO сквозь `ensure`» (**Should**, §16) описано в §5.1 и реализовано в T-173 (#290): хвост тела `trap` с `ensure` компилируется в `TAILCALLENS`. `TAILCALL` внутри `trap` не эмитится, тело `trap` без `ensure` не хвостовое.

Для `match`/`with`, когда они появятся (сегодня они вне компилятора, K-8), правило то же: ветки `match` и последний стейтмент/тело `with` наследуют `dest.tail`, тело `with … else` тоже.

**Сочетание с trap.** Инвариант компилятора: `TAILCALL` не эмитится при `trapDepth > 0`. `Verify` проверяет то же линейным проходом: `TRAPBEGIN` даёт `+1`, `TRAPEND` даёт `-1`, `TAILCALL` при глубине > 0 — ошибка. Регионы trap непрерывны и вложены, поэтому линейного прохода достаточно. VM дополнительно проверяет `len(f.handlers) == 0` и иначе завершает актор внутренней ошибкой. Хвостовой вызов из `trap` с `ensure` — отдельный опкод `TAILCALLENS` со своим правилом `Verify` (§5.1); правило для `TAILCALL` и `TAILCALLSPREAD` не меняется.

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
- `TRAPENSURE A sBx` — то же, но обработчик помечен `ensure: true`. `raise` ловится им так же, как `TRAPBEGIN`. Unwind от `exit` (§12.7) пропускает обычные обработчики и входит только в помеченные: так `ensure` выполняются, а `trap` сигнал не ловит.
- `TRAPEND` снимает верхний обработчик.
- `ENSEND` закрывает блок `ensure`. Если unwind от `exit` вошёл именно в этот блок (тот же кадр, та же глубина `handlers`), `ENSEND` продолжает unwind; иначе это пустая инструкция.
- `MAKEOK A B` даёт `R[A] = Ok(R[B])`, `MAKEERROR A B` даёт `R[A] = Error(R[B])`.
- При `raise` в кадре VM снимает обработчик, записывает `regs[errReg] = val` и переходит на `ip`. Если `raise` пришёл из вызванного кадра, то же делает `tryUnwindRaise`.

**Схема без `ensure`.** Регистр ошибки и регистр результата совпадают с целевым `T`. Это безопасно: `T` на входе в `trap` мёртв. Inline-форма `trap(expr)` компилируется так же.

```
      TRAPBEGIN  T  →H
      <inner → T>                 ; НЕ хвостовая позиция
      TRAPEND
      MAKEOK     T  T
      JMP        →END
H:    MAKEERROR  T  T
END:
```

**Схема с `ensure`** (`compileTrapWithEnsure`). `E` (ошибка) и `F` (флаг «была ошибка») — временные выше `T`. Флаг `Bool` заменяет прежнее сравнение с атомом `:no_error` (D-5). `G_i` — флаг регистрации `ensure_i`: он становится `true` в текстовой точке `ensure_i` внутри тела, и в блоке ENS выполняются только зарегистрированные ensure (I-F5, T-37). `L_j` — слоты let-связей тела: они выделены до `TRAPENSURE`, поэтому живы на пути через `BH`, и ensure может их читать.

```
      LOADK      T  #()           ; предынициализация: Verify видит T на пути BH
      LOADK      E  #()
      LOADK      F  #false
      LOADK      G_i #false       ; для каждого ensure_i
      LOADK      L_j #()          ; для каждой let-связи тела
      TRAPENSURE E  →BH
      <тело → T>                  ; не хвост (исключение — §5.1);
                                  ; в точке ensure_i: LOADK G_i #true
      TRAPEND
      JMP        →ENS
BH:   LOADK      F  #true         ; E уже содержит ошибку тела
ENS:                              ; ensure — в порядке, обратном тексту (LIFO, §10.3)
   для i = last..first:
      JMPIFNOT   G_i →NEXT_i      ; не зарегистрирован — пропустить
      TRAPENSURE E  →EH_i         ; ошибка ensure перезапишет E: побеждает последняя
      <ensure_i → S>              ; S — временный регистр
      TRAPEND
      JMP        →NEXT_i
EH_i: LOADK      F  #true
NEXT_i:
      ENSEND                      ; конец блока: unwind от exit продолжается отсюда
      JMPIF      F  →ERR
      MAKEOK     T  T
      JMP        →END
ERR:  MAKEERROR  T  E
END:
```

Эталон — `testdata/bytecode/trap_ensure.txt`.

**LIFO и «побеждает последняя».** Ensure исполняются от последнего по тексту к первому. Каждый защищён собственным `TRAPENSURE`, поэтому падение одного не мешает остальным. Записывать в `E` может только сработавший обработчик, значит побеждает **последняя по времени** ошибка. Это то же, что и раньше (`TestTrapEnsureLifo*`, `TestEnsureAllRunOnFailure`).

**Регистры зафиксированы.** Обработчик знает только `errReg`. Компилятор гарантирует, что на входе в код-обработчик живы лишь регистры, выделенные до `TRAPBEGIN`/`TRAPENSURE`.

**Область активного `ensure`.** Это всё между первым `TRAPENSURE` и `ENSEND`, включая сами `ensure`. Все вызовы в ней — обычные `CALL` (§4), кроме хвоста тела по схеме §5.1.

---

### 5.1 TCO сквозь `ensure` (cleanup-регистр)

**Статус.** **Should** (§16), реализовано в T-173 (#290): компилятор — `emitTailCallEns`, VM — `internal/vm/drain.go`. Норматив §10.3 и §15.3 не меняется: вызовы в области активного `ensure` TCO не **гарантируют**, а эталонная VM его даёт в описанном ниже случае. Решения приняты в обсуждении #289.

**Что именно даёт схема.** Тело `trap` оборачивается в `Ok`/`Error`, а ensure каждого уровня обязан выполниться. Поэтому вызов в хвосте тела не хвостовой в строгом смысле: после его возврата остаётся работа. Схема переносит эту работу из кадра в компактную запись. Итог: **O(1) кадров** (`len(a.frames)` не растёт) и **O(n) памяти в куче** — одна запись с замыканиями ensure на уровень рекурсии. O(1) памяти без изменения семантики §10.3 недостижимо: n ensure с n разными захватами обязаны выполниться.

**Хвостовая позиция.** `TAILCALLENS` получает хвост последнего стейтмента тела `trap` с `ensure` при всех условиях:

1. сам `trap` стоит в хвостовой позиции функции (§4, `dest.tail`);
2. ни один `ensure` этого `trap` не стоит текстом после последнего стейтмента тела;
3. вызов не spread (`f(..xs)` остаётся `CALLSPREAD`).

Ветки `if`/`recv`/`match` внутри тела наследуют хвостовость, как в §4. По-прежнему **не хвостовые**: `r = trap …` (RHS `let`, в том числе с последующим `r`), вложенный `trap` (в хвосте тела внешнего `trap`), inline `trap(…)`, `trap` без `ensure`.

Условие 2 сохраняет семантику регистрации: ensure после последнего стейтмента регистрируется только после возврата вызова, и если вызов бросит `raise`, этот ensure не выполнится. Ранняя регистрация на tail-сайте изменила бы это поведение.

К tail-сайту все ensure этого `trap` зарегистрированы, и это известно статически (условие 2). Поэтому набор замыканий на tail-сайте фиксирован: все ensure `trap` в порядке LIFO.

**Хранение: `Frame.cleanups`.** «cleanup-регистр» из §16 — Go-слот кадра, а не регистр `R[k]`: `TAILCALL` очищает `regs`, а слот переживает замену кадра. Регистры и `Verify` этот слот не видят.

```go
type Frame struct {
 // … поля §2 …
 cleanups []cleanupRec // LIFO; верхняя запись — самый внутренний уровень
}

// cleanupRec — один логический уровень trap с ensure, чей кадр заменён
// хвостовым вызовом.
type cleanupRec struct {
 ensures []*runtime.ClosureValue // замыкания ensure, уже в порядке исполнения (LIFO)
}
```

**Форма ensure: замыкания только на tail-сайте.** Обычный путь (блок ENS в §5) не меняется и ничего не платит. Только перед `TAILCALLENS` компилятор строит по замыканию на каждый ensure: тело — выражение ensure, захваты — нужные ему локали по значению (K-7). Захват по значению корректен: связывание единственное, shadowing внутри одной области запрещён (§0 #12), поэтому в момент выхода из `trap` ensure увидел бы те же значения. Функция замыкания компилируется как лямбда без параметров и получает имя по правилу лямбд.

**Опкод `TAILCALLENS A B C`** (ABC).

| Операнд            | Смысл                                                       |
| ------------------ | ----------------------------------------------------------- |
| `R[A]`             | callee                                                      |
| `R[A+1..A+B]`      | аргументы (инвариант I-2, как у `TAILCALL`)                 |
| `R[A+B+1..A+B+C]`  | замыкания ensure в порядке исполнения (LIFO), `C ≥ 1`       |

Семантика по шагам:

1. Снять верхний обработчик. Это обязан быть `TRAPENSURE` этого `trap`, и он единственный (`len(f.handlers) == 1`, иначе `internal:`).
2. Положить `cleanupRec{ensures: копия R[A+B+1..A+B+C]}` в `f.cleanups`. Копируются указатели `ClosureVal`, а не `Value`: `runtime.Value` занимает около 300 байт, и запись на уровень рекурсии стоила бы столько на каждый ensure. Не-замыкание в окне — `internal:`.
3. Дальше — как `TAILCALL` (§4): native вызывается сразу, байткод-функция заменяет кадр; `f.callDst` не трогается, `f.cleanups` переносится.

Порядок 1–2 до разбора callee важен. Ошибка `resolveCallee`/`checkArity` или `raise` из native бросается уже после того, как запись лежит в `cleanups`. Её ловит эта запись (см. «Drain»), и результат совпадает с `CALL` внутри тела: `Error(e)` уровня.

**Drain: исполнение записей.** Записи исполняет служебный drain-кадр на механизме возобновляемых нативов (`nativeCont`, T-58). Его расширение для этой схемы: `raise` из вызванного колбэка доставляется в cont (`drainRun.catch`, из `tryUnwindRaise`/`unwindAbove` и из `stepNative`), а не всплывает сквозь кадр; `nativeStep.exit` сообщает, что drain в режиме exit закончил. Drain-кадр заменяет кадр с `cleanups`, наследует его `callDst` и записи и вызывает замыкания ensure обычными кадрами.

Drain запускается, когда кадр с непустым `cleanups`:

- выполняет `RETURN v` — тело внутреннего уровня вернуло `v`;
- завершает хвостовой вызов native с результатом `v` (`TAILCALL` или `TAILCALLENS` с native callee);
- получает `raise e`, а собственных `handlers` у кадра нет (из самого кадра или из вызванного, через `tryUnwindRaise`). Собственные `handlers` принадлежат текущей функции и срабатывают **первыми**; `cleanups` принадлежат логически внешним уровням;
- проходит unwind от `exit` (ниже).

Алгоритм для `RETURN` и `raise` в точности повторяет схему §5 для каждого уровня:

```
body := (val: v, err: false)        ; RETURN v / native tail
     или (val: e, err: true)        ; raise e без собственных handlers
пока cleanups не пуст:
    rec := снять верхнюю запись
    F, E := body.err, body.val
    для ens в rec.ensures:          ; уже LIFO
        вызвать ens(); raise e2 → F, E := true, e2   ; побеждает последняя
    level := F ? Error(E) : Ok(body.val)
    body := (val: level, err: false) ; результат уровня — значение тела внешнего
результат кадра := body.val → callDst вызывающего
```

Для глубины 3 и `RETURN :done` результат — `Ok(Ok(Ok(:done)))`, как без TCO. `raise e` на уровне k даёт `Ok^(k-1)(Error(e))`, и ensure выполняются в том же порядке, что без TCO.

**`exit` и `:kill` (§12.7).** `unwindExit` сначала проходит `handlers` кадра (только помеченные `ensure`), затем `cleanups` кадра, и лишь потом снимает кадр. Записи исполняет тот же drain в режиме exit: выполняются все ensure всех записей LIFO, `trap` сигнал не ловит, ошибка ensure причину не меняет, по окончании unwind продолжается. `exit(pid, :kill)` пропускает `cleanups`, а начатый drain прерывает. Если `exit` приходит, пока drain исполняет ensure обычного `RETURN`, drain переходит в режим exit с текущего места: оставшиеся ensure выполняются, как для блока ENS в §5.

**`Verify`.** Линейный проход §4 хранит стек видов открытых регионов вместо одного счётчика:

- `TAILCALLENS` допустим, только если открыт ровно один регион и он открыт `TRAPENSURE`; иначе ошибка;
- `C ≥ 1`; все регистры `R[A..A+B+C]` `< NumRegs` и определены (definite assignment);
- в CFG `TAILCALLENS` — завершающая инструкция, как `TAILCALL`: рёбер наружу нет;
- правило для `TAILCALL`/`TAILCALLSPREAD` (только вне регионов) не меняется.

Регион, в котором стоит `TAILCALLENS`, в линейном коде закрывается обычным `TRAPEND` на другом пути (ветка без хвостового вызова), поэтому баланс `TRAPBEGIN`/`TRAPEND` не меняется.

**Учёт ресурсов.**

- Редукции (K-4): `TAILCALLENS` в байткод-функцию — одна, как `TAILCALL`; каждый вызов замыкания ensure из drain — одна, как `CALL`.
- Бюджет хода (§12.10): замыкания списывают `turn_alloc_bytes` через `MAKECLOSURE`; `TAILCALLENS` дополнительно списывает размер `cleanupRec` (`chargeBytes`); обёртки `Ok`/`Error` уровней drain списываются как у `MAKEOK`. Бесконечная рекурсия в `trap` с `ensure` упирается в бюджет памяти актора, а не в OOM процесса.

**Stack trace.** Кадры, заменённые хвостовым вызовом, в trace не попадают (§12, T-79). Замыкание ensure видно в trace как лямбда. Drain-кадр не виден, как и кадры `nativeCont`.

**Что меняется — полный список.**

| Элемент                              | Изменение                                                                                           |
| ------------------------------------ | --------------------------------------------------------------------------------------------------- |
| `TAILCALLENS A B C`                  | новый опкод (дизассемблер: `TAILCALLENS r5 1 1`)                                                    |
| `Frame.cleanups`, `cleanupRec`       | новое поле и тип; `TAILCALL`/`TAILCALLENS` переносят поле, снятие кадра его обнуляет                 |
| drain-кадр                           | новый `nativeCont` (`drainRun`); механизм cont учится получать `raise` колбэка; `nativeStep.exit`   |
| `stepFrame`                          | обёртка над исполнением кадра: `stepDone` (`RETURN`, хвостовой native) и непойманный raise кадра с `cleanups` превращают его в drain |
| `tryUnwindRaise`, `unwindAbove`, `raiseCatchable` | кадр без `handlers`, но с `cleanups`, ловит raise вызванного кадра: drain с `Error`; drain-кадр ловит raise своего ensure |
| `unwindExit`                         | после помеченных `handlers` исполняет `cleanups` (кроме `:kill`)                                    |
| `verifyTailCall`, CFG `Verify`       | стек видов регионов; правило `TAILCALLENS`; `TAILCALLENS` — завершающая                             |
| компилятор                           | `dest.ens` из `compileTrapWithEnsure` (`ensureTailEligible`); `emitInvoke` → `emitTailCallEns`: `MAKECLOSURE` на каждый ensure + `TAILCALLENS` |
| бюджет                               | `chargeBytes` — списание байт без значения                                                           |
| **не меняются**                      | `TRAPBEGIN`, `TRAPENSURE`, `TRAPEND`, `ENSEND`, `MAKEOK`, `MAKEERROR`, `TAILCALL`, `TAILCALLSPREAD`; блок ENS §5 |

**Байткод «до/после».** Схематично (номера регистров и констант — ориентир; точный вид — `testdata/bytecode/tail_ensure.txt`):

```brig
fn loop(n) ->
    trap
        ensure log(n)
        if n == 0 then :done else loop(n - 1)
```

До (сегодня): `loop(n - 1)` — `CALL`, каждый уровень держит кадр.

```
      LOADK       r1 #()          ; T
      LOADK       r2 #()          ; E
      LOADK       r3 #false       ; F
      LOADK       r4 #false       ; G0 — флаг ensure log(n)
      TRAPENSURE  r2 →BH
      LOADK       r4 #true        ; регистрация ensure log(n)
      <n == 0 → r5>
      JMPIFNOT    r5 →ELSE
      LOADK       r1 :done
      JMP         →BODYEND
ELSE: GETGLOBAL   r5 "loop"
      <n - 1 → r6>
      CALL        r5 1 → r1       ; кадр loop остаётся
BODYEND:
      TRAPEND
      JMP         →ENS
BH:   LOADK       r3 #true
ENS:  JMPIFNOT    r4 →NEXT0
      TRAPENSURE  r2 →EH0
      GETGLOBAL   r6 "log"
      MOVE        r7 r0
      CALL        r6 1 → r5
      TRAPEND
      JMP         →NEXT0
EH0:  LOADK       r3 #true
NEXT0:
      ENSEND
      JMPIF       r3 →ERR
      MAKEOK      r1 r1
      JMP         →END
ERR:  MAKEERROR   r1 r2
END:  RETURN      r1
```

После: меняется только ветка `ELSE`, остальной код совпадает.

```
ELSE: GETGLOBAL   r5 "loop"
      <n - 1 → r6>
      LOADK       r8 <fn ensure log(n)>
      MOVE        r9 r0           ; захват n
      MAKECLOSURE r7 r8 1         ; R[A+B+1] = замыкание ensure
      TAILCALLENS r5 1 1          ; снять TRAPENSURE, push cleanupRec, замена кадра
```

Ветка `:done` идёт обычным путём через `TRAPEND` и блок ENS. На самом внутреннем уровне это даёт `Ok(:done)`, затем `RETURN` запускает drain по записям внешних уровней.

**Инварианты — тест-якоря T-173.**

1. Рекурсия глубиной 10⁶ внутри `trap` с `ensure` (`TestTailCallThroughEnsureDeepRecursion`): `len(a.frames)` не растёт. Результат `Ok^n(:done)` тест разворачивает циклом и целиком не печатает.
2. Каждый ensure выполняется ровно один раз, LIFO: от самого внутреннего уровня к внешнему (`TestEnsureOrderWithTailCall`).
3. `raise` на уровне k даёт `Ok^(k-1)(Error(e))` и тот же порядок ensure, что без TCO. Проверяется дифференциально: та же программа с `r = trap …; r` (без TCO) даёт тот же результат и тот же лог (`TestTailCallEnsureRaiseMatchesNonTail`: raise в кадре, в вызванной функции, не-функция, неверная арность байткода и native).
4. Ошибка в ensure на уровне k: побеждает последняя ошибка уровня, остальные ensure всех уровней выполняются (`TestTailCallEnsureFailureWins`).
5. `exit` посреди рекурсии выполняет все n ensure LIFO, причина `:down` не меняется; `exit` во время drain обычного возврата переводит его в режим exit; `exit(pid, :kill)` пропускает `cleanups` и прерывает drain (`TestTailCallEnsureExitAndKill`).
6. `trap` с `ensure` без хвостового вызова: bytecode-golden `trap_ensure.txt` не меняется, без `MAKECLOSURE` и `TAILCALLENS` (обычный путь не платит за фичу).
7. `TestAuditNoTailCallInsideTrap`: `f_inline` и `f_block` по-прежнему без `TAILCALL*`; `f_ensure` получает `TAILCALLENS`, а не `TAILCALL`. `TestTailCallEnsEligibility`: условия 1–3 хвостовой позиции.

В тесте 10⁶ пик кучи — около 700 МБ, из них около 400 МБ — сам результат `Ok^n(v)`; без TCO к нему добавились бы 10⁶ кадров.

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

**Модель планирования (эталон).** Планировщик — один кооперативный run-loop в одной goroutine (`runSlice`): актор — запись `*Actor` со стеком кадров, а не goroutine. Готовые акторы берутся из ready-очереди по очереди; актор вытесняется по исчерпании квантума редукций (K-4). Порядок исполнения воспроизводим, кроме зависимости от wall-clock (таймеры). Спека (§15.2, решение #40, вариант C) гарантирует лишь G1–G4: FIFO в паре отправитель→получатель, `:down` впереди и вне HWM, fairness по редукциям, порядок таймеров (deadline, порядок взвода). Модель потоков — деталь реализации: эволюция в N:M (с режимом `schedulers=1`, эквивалентным эталону) возможна без правки тира 1; goroutine-per-actor не планируется.

**Механизмы Wave 12 (T-160, спека §12.7–§12.10).** Здесь — только список и контракт с VM; кодирование (опкод или native, формат операндов) выбирает задача реализации.

| Примитив | Где | Контракт с VM | Задача |
| --- | --- | --- | --- |
| `exit(pid, reason)` | native | Ставит жертве флаг сигнала с причиной (первый выигрывает, `:kill` перекрывает) и будит её, если она ждёт в `RECVTAKE` или `await`. Флаг проверяется в каждой точке редукции (K-4) и при пробуждении; сработал — unwind, который пропускает кадры `trap` и выполняет `ensure` (кроме `:kill`), затем обычная смерть: `:down` наблюдателям, снятие имён, сброс слотов. `exit(self(), r)` — сразу, без ожидания точки редукции. | T-163 |
| `spawn_watched(f)`, `spawn_watched(f, limits)` | `SPAWN` с флагом или native | Создание актора и взвод наблюдения — один шаг планировщика, без редукции между ними. | T-163 |
| `spawn`/`spawn_linked` с `limits` | как без `limits` | Лимиты хранятся в `Actor`; проверка — там же, где счёт редукций. | T-169 |
| `register`/`unregister`/`whereis` | native | Таблица `имя → Pid` в `Scheduler`, ключ сравнивается как ключ `Map`; обратный индекс `Pid → имена` снимается в том же шаге, где ставятся `:down`. | T-164 |
| `make_ref()` | `MAKEREF` | Ref получает владельца (текущий актор); слот открыт. | T-165 |
| `reply(pid, ref, v)` | native | Кладёт `v` в слот `ref` у `pid`, если слот открыт и пуст; иначе ничего. Ящик, HWM и `mailbox_size` не трогаются. Будит `pid`, если он ждёт этот `ref`. | T-165 |
| `await(ref, timeout)` | блокирующий опкод по образцу `RECVTAKE` | Слот заполнен → `Ok(v)`, слот закрыт. Иначе взвести таймер в общей куче таймеров (G4) и вернуть `stepBlock`; пробуждение — `reply` или таймаут (`Error(:timeout)`); `exit` прерывает ожидание. Новые рёбра — в `vm.Verify`. | T-165 |
| `Actor.info(pid)` | native | Читает счётчики `Actor`: редукции и байты всего и за текущий ход, размер ящика. | T-169 |
| `Timer.send_after(ms, pid, msg)`, `Timer.cancel(ref)` | native | Запись в общей куче таймеров (G4) с типом «доставить»: deadline от монотонных часов, `msg`, `pid`, порядок взвода. Срабатывание — `send` без отправителя; мёртвый `pid` и HWM — сообщение отбрасывается. `cancel` снимает запись, если она в куче. Запись принадлежит VM и переживает актора. | T-166 |
| `Time.monotonic_ms()`, `Time.now()` | native | Читают тот же монотонный источник, что куча таймеров, и стенные часы (мс Unix UTC). | T-166 |
| `Global.put`/`Global.get` | native | Таблица `имя → Value` в `Scheduler`, ключ сравнивается как ключ `Map`; значения неизменяемы, заменяется только запись. | T-167 |
| `Signal.subscribe(names)` | native | Создаёт `Port` (владелец — вызвавший актор), регистрирует его в реализации порта за интерфейсом (см. ниже). | T-168 |
| `Port.close(port)` | native | Только владелец: порт помечается закрытым, реализация порта освобождает ресурс; идемпотентно. | T-168 |
| `Sys.halt(code)` | native | Ставит планировщику флаг остановки с кодом; run-loop выходит на ближайшей итерации, `ensure` не выполняются, порты закрываются. | T-168 |
| `Port.request(port)` | native | Потоковый порт: ставит флаг «запрос активен» и передаёт запрос ресурсу; флаг уже стоит — ничего. Флаг снимает run-loop, когда кладёт ответ (`:port_data`, `:port_eof`, `:port_error`) в ящик. | T-228 |
| `Port.write(port, data)` | native | Сплющивает iodata в байты на run-loop, прибавляет длину к счётчику недописанного и передаёт ресурсу. Счётчик не меньше порога — `Error(:busy)` и флаг «обещан `:port_ready`». Ресурс сообщает о записанном служебным событием inject-очереди; run-loop уменьшает счётчик и, если флаг стоит и счётчик ниже порога, кладёт `:port_ready`. | T-228 |
| `Port.give(port, pid)` | native | Меняет владельца в описателе порта и переносит порт в список портов нового владельца. Событие берёт владельца в момент разбора inject-очереди, поэтому активный запрос переходит с портом. | T-228 |
| `File.open(path, mode)` | native | Создаёт потоковый порт и отдаёт открытие реализации `File` за интерфейсом; ресурс открывает файл в своей goroutine. | T-228 |
| `HttpServer.listen(addr)` | native | Проверяет форму `host:port`, создаёт потоковый порт-слушатель и отдаёт адрес реализации `HttpServer` за интерфейсом (`HTTPHub`); адрес занимается в goroutine ресурса. `Port.request` на слушателе — «отдай один запрос»; ответ ресурса — событие с новым запросом, из которого run-loop при разборе inject-очереди создаёт порт запроса (владелец — владелец слушателя в этот момент) и кладёт `(:http_request, listener, req, head)`. | T-229 |
| `HttpServer.respond(req, status, headers)` | native | Проверяет статус и заголовки на run-loop, ставит порту запроса флаг «ответ начат» и разрешает запись (кроме `204`/`304`), передаёт команду ресурсу. | T-229 |

**Механизмы Wave 12 (T-162, спека §12.12, §15.2): внешние события.**

- **Inject-очередь.** Потокобезопасная очередь `(порт, значение)` в `Scheduler`; единственный вход для событий, порождённых не акторами. Ресурс ждёт в собственной goroutine (реализация порта) и кладёт событие в очередь; порядок событий одного порта — порядок `push` из одной goroutine ресурса (G5).
- **Ожидание вместо выхода.** Когда ready-очередь пуста, run-loop блокируется на `select { ближайший таймер | inject }`, а не возвращает «deadlock». Событие из очереди run-loop разбирает сам: порт закрыт — событие отбрасывается (после `Port.close` или смерти владельца ничего не доставляется, гонки нет: состояние порта читает только run-loop); иначе значение кладётся в конец ящика владельца, минуя HWM, и владелец будится.
- **Условие выхода.** Счётчик открытых портов растёт при создании и убывает при закрытии. Выход: `main` завершился нормально и счётчик 0; `main` упал; `Sys.halt`. Ready пуст и нет ни таймеров, ни портов, от которых событие возможно (`Signal`; потоковый — активный запрос, недописанные данные, обещанный `:port_ready`): `main` не завершился — прежняя ошибка `deadlock`, завершился — выход (T-228).
- **Закрытие и обрыв (T-229).** Порт закрывается двумя путями: явно (`Port.close`, событие `:port_eof`/`:port_error`) — ресурс получает `Close`; иначе (смерть владельца, выход программы, `Sys.halt`) — ресурс, который это различает, получает `Abort`. HTTP: `Abort` порта запроса — `500` или обрыв соединения, `Abort` слушателя — дренаж не дольше 5 с; `File` различия не делает и дописывает принятое. Событие ресурса, чей порт закрыт, run-loop отбрасывает; запросы, которые слушатель отдал, но run-loop не принял (`Start` не вызван), ресурс сам закрывает `503`.
- **Дозапись при закрытии (T-228).** Закрытие потокового порта отдаёт ресурсу «допиши и закрой» и не ждёт. Счётчик закрывающихся ресурсов в `Scheduler` убывает, когда ресурс закончил; выход из программы (и `Sys.halt`) ждёт, пока он станет 0. Смерть актора закрывает его порты в том же шаге, где ставятся `:down`, снимаются имена и слоты (`exit`, §12.7).
- **Ядро без ОС (R14).** Ядро VM не импортирует `os/signal`, `net`, `net/http`, `os/exec` и подобное: `Scheduler` видит порт как интерфейс (открыть, закрыть, канал событий в inject-очередь), реализации живут в отдельных файлах, а `cmd/brig` их подключает. Ядро компилируется под `js/wasm` без правок.
- **`vm.Verify`.** Новых опкодов нет (все примитивы — native); рёбер потока управления это не меняет.

Бюджет хода: счётчики хода в `Actor` обнуляются, когда `RECVTAKE` берёт сообщение или уходит на ветку `after`; превышение — тот же флаг сигнала, что у `exit`, с причиной `(:resource_limit, (kind, used, limit))`. Точность учёта байт — оценка (не точнее Go-аллокатора); без лимитов проверка не должна заметно стоить (порог — T-152).

**Механизмы Wave 15 (T-220, спека §12.13, §12.14): интроспекция и события VM.** Только список и контракт; кодирование выбирает задача реализации.

| Примитив | Где | Контракт с VM | Задача |
| --- | --- | --- | --- |
| `Actor.list()` | native | Живые акторы из таблицы `Scheduler`; актор уходит из неё в том же шаге, где ставятся `:down`. | T-221 |
| `Actor.info(pid)` | native | К счётчикам T-169: первое связанное имя из обратного индекса реестра, статус (`:running` — ready или текущий, `:recv` — blocked в `RECVTAKE`, `:waiting` — blocked в `await`), множества наблюдателей и наблюдаемых (по живым ref `watch`), имя начальной функции в формате trace. | T-221 |
| `(*Scheduler).Snapshot()` | Go-API | Снимок таблицы акторов (pid, имя, статус, ящик, редукции, связи) для консоли; берётся на goroutine планировщика, таблицу с чужой goroutine не читает. | T-221 |
| `Telemetry.attach`/`detach`/`emit` | native или stdlib поверх native | Реестр подписок — таблица VM, общая для всех акторов, упорядоченная по `attach`; без подписок `emit` — одна проверка. Вызов обработчика — обычный вызов функции в текущем акторе (редукции и бюджет — его); `raise` из обработчика ловится, подписка снимается, излучается `[:telemetry, :handler, :failed]`. | T-222 |

Точки излучения событий VM (все — после проверки «есть ли подписки»; без подписок ничего не собирается):

- `[:vm, :spawn]` — в `SPAWN` и нативах `spawn*` после создания актора и взвода наблюдения, в кадре создателя, до записи результата.
- `[:vm, :mailbox, :hwm]` — в `SEND` на ветке `Error(:busy)`, в кадре отправителя; при срабатывании таймера доставки в полный ящик — через служебный актор.
- `[:vm, :actor, :crash]` и `[:vm, :actor, :down]` — в шаге смерти, после постановки `:down`, снятия имён, слотов и портов: VM кладёт событие в очередь служебного актора телеметрии (обычный актор, без имени в реестре), он вызывает обработчики. `:crash` — только для `actorFailed` от `ErrRaise`, с `Trace` (T-79), и ставится перед `:down`.
- Служебный актор создаётся лениво, при первом событии смерти или таймера, у которого есть подписка. Его собственная смерть событий не излучает.

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
| `OpJumpFalse`                                                    | **`JMPIFNOT`**         | AsBx   | Переход, если `R[A] == Bool(false)`; не-`Bool` — ловимый `:type_error` (K-2)                                                |
| `OpJumpTrue`                                                     | **`JMPIF`**            | AsBx   | Переход, если `R[A] == Bool(true)`; не-`Bool` — ловимый `:type_error` (K-2)                                                 |
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

### 11. Риски

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
- `TAILCALL` вне регионов trap (линейный счётчик глубины; правило `TAILCALLENS` — §5.1);
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

---

## Открытые вопросы

Решаются в коде, по измерениям:

1. **Пул `regs` в `Actor`** (free-list по размеру кадра). По умолчанию не включать. Решить по профилю `TestTailRecursion` и `fib(27)` после миграции.
2. **`Verify` (dataflow-проход) в `brig run` по умолчанию.** По умолчанию только тесты и `BRIG_VERIFY=1`. Решить по замеру стоимости на `examples/*.brig`.
3. **Аудит `Arity` native в прелюдии.** D-2 превратит неверно объявленную арность из молчаливого дефекта в ошибку. Выяснится при прогоне S7.6.

## История

План миграции со стековой VM на регистровую (Sprint 7: подэтапы S7.1–S7.7, порядок отладки первого зелёного прогона, оценка по дням) и правки документации при снятии отступления от §15.1 выполнены и вмержены в `main` задачей T-07 (#7). Текст плана из документа убран; он есть в git — `git show 02cf66c:docs/02-register-based-virtual-machine.md`, разделы 11, 13 и «Оценка по подэтапам». Состояние до миграции — тег `stack-vm-final`, после — `regvm-merged`.
