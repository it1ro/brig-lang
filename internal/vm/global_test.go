package vm_test

import "testing"

// TestGlobalPutGet — put возвращает (), get — Some(значение) или None (§12.11).
func TestGlobalPutGet(t *testing.T) {
	runModuleSync(t, `module Main
fn main() ->
    assert(Global.get(:config) == None)
    assert(Global.put(:config, 7) == ())
    assert(Global.get(:config) == Some(7))
    assert(Global.get((:limits, :http)) == None)
    Global.put((:limits, :http), 3)
    assert(Global.get((:limits, :http)) == Some(3))
    :ok
`)
}

// TestGlobalVisibleAcrossActors — таблица общая для акторов, не стейт актора.
func TestGlobalVisibleAcrossActors(t *testing.T) {
	runModuleSync(t, `module Main
fn writer(parent) ->
    Global.put(:config, %{:retries => 3})
    send(parent, :wrote)

fn reader(parent) ->
    recv
        :go -> send(parent, Global.get(:config))

fn main() ->
    assert(Global.get(:missing) == None)
    me = self()
    _w = spawn(() -> writer(me))
    r = spawn(() -> reader(me))
    recv
        :wrote -> ()
    send(r, :go)
    recv
        v -> assert(v == Some(%{:retries => 3}))
    :ok
`)
}

// TestGlobalOverwrite — put заменяет привязку; прочитанное значение не меняется.
// 1 и 1.0 — один ключ, dec"1" — другой (§4.8).
func TestGlobalOverwrite(t *testing.T) {
	runModuleSync(t, `module Main
fn main() ->
    Global.put(:config, 1)
    seen = Global.get(:config)
    Global.put(:config, 2)
    assert(seen == Some(1))
    assert(Global.get(:config) == Some(2))
    Global.put(1, :int)
    Global.put(1.0, :float)
    assert(Global.get(1) == Some(:float))
    assert(Global.get(1.0) == Some(:float))
    Global.put(dec"1", :dec)
    assert(Global.get(dec"1") == Some(:dec))
    assert(Global.get(1) == Some(:float))
    :ok
`)
}

// TestGlobalNonKey — имя, не равное себе, не ключ Map: get после put
// его бы не увидел (§12.11).
func TestGlobalNonKey(t *testing.T) {
	runModuleSync(t, `module Main
fn is_put(r) ->
    match r
        Error((:type_error, (:put, v))) -> v != v
        _ -> false

fn is_get(r) ->
    match r
        Error((:type_error, (:get, v))) -> v != v
        _ -> false

fn main() ->
    inf = 1.0e308 * 10.0
    nan = inf - inf
    assert(is_put(trap(Global.put(nan, 1))))
    assert(is_get(trap(Global.get(nan))))
    assert(is_put(trap(Global.put((:a, nan), 1))))
    assert(Global.get(:a) == None)
    :ok
`)
}
