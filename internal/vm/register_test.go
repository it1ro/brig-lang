package vm_test

import "testing"

// TestRegisterWhereis — имя связывается с pid, whereis находит; у одного
// pid несколько имён; unregister идемпотентен (§12.8).
func TestRegisterWhereis(t *testing.T) {
	runModuleSync(t, exitPrelude+`
fn main() ->
    pid = spawn(() -> idle())
    assert(whereis(:repo) == None)
    assert(register(:repo, pid) == Ok(()))
    assert(register(:repo2, pid) == Ok(()))
    assert(whereis(:repo) == Some(pid))
    assert(whereis(:repo2) == Some(pid))
    assert(unregister(:repo) == ())
    assert(unregister(:repo) == ())
    assert(whereis(:repo) == None)
    assert(whereis(:repo2) == Some(pid))
    assert(register(:me, self()) == Ok(()))
    assert(whereis(:me) == Some(self()))
    :ok
`)
}

// TestRegisterTermKey — имя — любой терм, сравнение как у ключей Map.
func TestRegisterTermKey(t *testing.T) {
	runModuleSync(t, exitPrelude+`
fn main() ->
    a = spawn(() -> idle())
    b = spawn(() -> idle())
    assert(register((:checker, 42), a) == Ok(()))
    assert(register((:checker, 43), b) == Ok(()))
    assert(register([1, 2], b) == Ok(()))
    assert(whereis((:checker, 42)) == Some(a))
    assert(whereis((:checker, 43)) == Some(b))
    assert(whereis([1, 2]) == Some(b))
    assert(whereis((:checker, 44)) == None)
    :ok
`)
}

// TestRegisterAlreadyRegistered — занятое имя (даже тем же pid) и мёртвый
// pid дают Error; не-Pid — :type_error.
func TestRegisterAlreadyRegistered(t *testing.T) {
	runModuleSync(t, exitPrelude+`
fn main() ->
    a = spawn(() -> idle())
    b = spawn(() -> idle())
    assert(register(:n, a) == Ok(()))
    assert(register(:n, b) == Error(:already_registered))
    assert(register(:n, a) == Error(:already_registered))
    assert(whereis(:n) == Some(a))
    (dead, ref) = spawn_watched(() -> :done)
    take()
    assert(register(:d, dead) == Error(:not_alive))
    assert(whereis(:d) == None)
    r = trap
        register(:x, 1)
    assert(r == Error((:type_error, (:register, 1))))
    :ok
`)
}

// TestRegisterRemovedOnDeath — имена сняты в той же редукции, что и :down:
// получив :down, актор уже видит None (нормальный конец, raise, exit).
func TestRegisterRemovedOnDeath(t *testing.T) {
	runModuleSync(t, exitPrelude+`
fn die_on_msg() ->
    recv
        _ -> raise(:boom)

fn main() ->
    (b, rb) = spawn_watched(() -> die_on_msg())
    assert(register(:b, b) == Ok(()))
    send(b, :go)
    take()
    assert(whereis(:b) == None)
    (c, rc) = spawn_watched(() -> idle())
    assert(register(:c, c) == Ok(()))
    assert(register((:c, 1), c) == Ok(()))
    assert(whereis(:c) == Some(c))
    exit(c, :shutdown)
    m = take()
    assert(whereis(:c) == None)
    assert(whereis((:c, 1)) == None)
    assert(register(:c, self()) == Ok(()))
    :ok
`)
}
