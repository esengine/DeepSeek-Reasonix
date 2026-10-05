"""Tests for topo.topological_sort.

Run with: python3 test_topo.py
"""

import signal

import topo


class _Timeout(Exception):
    pass


def _alarm_handler(signum, frame):
    raise _Timeout("topological_sort did not return within the time budget")


signal.signal(signal.SIGALRM, _alarm_handler)


def _run_with_timeout(fn, *args, **kwargs):
    signal.alarm(5)
    try:
        return fn(*args, **kwargs)
    finally:
        signal.alarm(0)


def _check_is_topo_order(n, edges, order):
    assert isinstance(order, list) and len(order) == n, (
        "expected a list of %d nodes, got %r" % (n, order))
    assert sorted(order) == list(range(n)), (
        "order %r is not a permutation of 0..%d" % (order, n - 1))
    pos = {node: i for i, node in enumerate(order)}
    for u, v in edges:
        assert pos[u] < pos[v], (
            "edge (%d, %d) violated: %d must come before %d" % (u, v, u, v))


def test_acyclic():
    n = 5
    edges = [(0, 1), (0, 2), (1, 3), (2, 3), (3, 4)]
    _check_is_topo_order(n, edges,
                         _run_with_timeout(topo.topological_sort, n, edges))
    print("ok - acyclic graph")


def test_disconnected():
    n = 4
    edges = [(1, 3)]
    _check_is_topo_order(n, edges,
                         _run_with_timeout(topo.topological_sort, n, edges))
    print("ok - disconnected graph")


def test_cycle_reported():
    try:
        _run_with_timeout(topo.topological_sort, 3, [(0, 1), (1, 2), (2, 0)])
        raise AssertionError(
            "expected ValueError for a cyclic graph, got a result")
    except ValueError:
        pass  # expected
    except _Timeout:
        raise AssertionError(
            "topological_sort looped forever on a cycle instead of reporting it")
    print("ok - cycle reported")


if __name__ == "__main__":
    test_acyclic()
    test_disconnected()
    test_cycle_reported()
    print("ALL TESTS PASSED")
