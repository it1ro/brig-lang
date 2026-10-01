package vm_test

import "testing"

// budgetPrelude — хелперы тестов бюджета хода (§12.10). take — следующее
// сообщение или :timeout; work(n) — n редукций без блокировки; grow —
// ход, который только выделяет память.
const budgetPrelude = `module Main
fn take() ->
    recv
        m -> m
    after 1000 -> :timeout

fn spin(n) -> spin(n + 1)

fn work(n) -> if n == 0 then :ok else work(n - 1)

fn grow(xs) -> grow([xs, xs, xs, xs])
`

// TestTurnReductionLimitExits — ход без конца завершается exit с причиной
// (:resource_limit, (kind, used, limit)), наблюдатель получает :down,
// ensure выполняются (§12.10).
func TestTurnReductionLimitExits(t *testing.T) {
	runModuleSync(t, budgetPrelude+`
fn guarded(parent) ->
    trap
        ensure send(parent, :cleaned)
        spin(0)

fn main() ->
    parent = self()
    (_pid, ref) = spawn_watched(() -> guarded(parent), { turn_reductions: 10_000 })
    down = take()
    (:down, r, (:resource_limit, (kind, used, limit))) = down
    assert(r == ref)
    assert(kind == :turn_reductions)
    assert(limit == 10_000)
    assert(used >= limit)
    assert(take() == :cleaned)

    (_p2, ref2) = spawn_watched(() -> grow([1]), { turn_alloc_bytes: 100_000 })
    (:down, r2, (:resource_limit, (kind2, used2, limit2))) = take()
    assert(r2 == ref2)
    assert(kind2 == :turn_alloc_bytes)
    assert(limit2 == 100_000)
    assert(used2 >= limit2)

    p3 = spawn(() -> spin(0), { turn_reductions: 5_000, turn_alloc_bytes: 1_000_000 })
    ref3 = watch(p3)
    assert(take() == (:down, ref3, (:resource_limit, (:turn_reductions, 5_000, 5_000))))

    p4 = spawn_linked(() -> spin(0), { turn_reductions: 2_000 })
    (:down, _r4, (:resource_limit, (:turn_reductions, _u4, 2_000))) = take()
    assert(mailbox_size(p4) == 0)
`)
}

// TestTurnBudgetLimitsValidated — неизвестное поле, не Int, Int <= 0 и не
// запись — ловимый (:type_error, (:spawn, limits)); актор не создаётся.
func TestTurnBudgetLimitsValidated(t *testing.T) {
	runModuleSync(t, budgetPrelude+`
fn bad(limits) ->
    match trap(spawn(() -> :ok, limits))
        Ok(_) -> :spawned
        Error(e) -> e

fn check(xs) ->
    if xs == [] then () else check_one(xs)

fn check_one([l, ..rest]) ->
    assert(bad(l) == (:type_error, (:spawn, l)))
    check(rest)

fn main() ->
    check([{ turn_steps: 10 }, { turn_reductions: 0 }, { turn_reductions: -5 },
        { turn_alloc_bytes: 1.5 }, { turn_reductions: "x" }, { mailbox_hwm: 0 },
        { mailbox_hwm: "x" }, (1, 2), [1]])
    assert(bad({}) == :spawned)
    assert(bad({ turn_reductions: 1, turn_alloc_bytes: 1 }) == :spawned)
    assert(bad({ mailbox_hwm: 1 }) == :spawned)
`)
}

// TestTurnBudgetResetsOnRecv — счётчики хода обнуляет взятие сообщения и
// срабатывание after: актор, который за жизнь тратит много больше лимита,
// но в каждом ходе меньше, живёт (§12.10). await ход не завершает.
func TestTurnBudgetResetsOnRecv(t *testing.T) {
	runModuleSync(t, budgetPrelude+`
fn done(parent, msg) ->
    send(parent, msg)
    recv
        :stop -> ()

fn server(parent, left) ->
    recv
        :job ->
            work(300)
            if left == 1 then done(parent, :served) else server(parent, left - 1)

fn ticker(parent, left) ->
    recv
        :never -> ()
    after 1 ->
        work(300)
        if left == 1 then done(parent, :ticked) else ticker(parent, left - 1)

fn awaiter(parent) ->
    ref = make_ref()
    work(300)
    await(ref, 1)
    work(300)
    await(ref, 1)
    work(300)
    send(parent, :awaited)

fn main() ->
    parent = self()
    (s, sref) = spawn_watched(() -> server(parent, 20), { turn_reductions: 1_000 })
    send_n(s, 20)
    assert(take() == :served)
    send(s, :stop)
    assert(take() == (:down, sref, :normal))

    (tk, tref) = spawn_watched(() -> ticker(parent, 10), { turn_reductions: 1_000 })
    assert(take() == :ticked)
    send(tk, :stop)
    assert(take() == (:down, tref, :normal))

    (_a, aref) = spawn_watched(() -> awaiter(parent), { turn_reductions: 800 })
    (:down, r, (:resource_limit, (:turn_reductions, _used, 800))) = take()
    assert(r == aref)

fn send_n(_pid, 0) -> ()
fn send_n(pid, n) ->
    send(pid, :job)
    send_n(pid, n - 1)
`)
}

// TestActorInfo — Actor.info(pid): Some(запись счётчиков) живого актора,
// None мёртвого и несуществующего; счётчики хода обнуляет recv, счётчики
// с рождения — нет (§12.10).
func TestActorInfo(t *testing.T) {
	runModuleSync(t, budgetPrelude+`
fn info() ->
    Some(i) = Actor.info(self())
    i

fn sleeper() ->
    recv
        :bye -> ()
        _ -> sleeper()

fn main() ->
    work(500)
    a = info()
    assert(a.reductions >= 500)
    assert(a.turn_reductions >= 500)
    assert(a.mailbox == 0)
    assert(a.alloc_bytes >= 0)
    assert(a.turn_alloc_bytes >= 0)

    xs = [1, 2, 3, 4, 5, 6, 7, 8]
    b = info()
    assert(b.alloc_bytes > a.alloc_bytes)
    assert(b.turn_alloc_bytes > a.turn_alloc_bytes)

    send(self(), :ping)
    send(self(), :pong)
    assert(info().mailbox == 2)
    assert(take() == :ping)
    c = info()
    assert(c.turn_reductions < 500)
    assert(c.turn_alloc_bytes < b.turn_alloc_bytes)
    assert(c.reductions >= a.reductions)
    assert(c.alloc_bytes >= b.alloc_bytes)
    assert(c.mailbox == 1)
    assert(len(xs) == 8)

    assert(take() == :pong)

    p = spawn(() -> sleeper())
    Some(pi) = Actor.info(p)
    assert(pi.mailbox == 0)
    ref = watch(p)
    send(p, :bye)
    assert(take() == (:down, ref, :normal))
    assert(Actor.info(p) == None)
    r = trap(Actor.info(:nope))
    assert(to_str(r) == "Error((:type_error, (:Actor.info, :nope)))")
`)
}
