"""Topological sort of a directed graph.

topological_sort(n, edges) returns a list of all n nodes (0..n-1) in a
topological order, i.e. for every edge (u, v), u appears before v.
Raises ValueError if the graph contains a cycle.
"""


def topological_sort(n, edges):
    graph = [[] for _ in range(n)]
    indegree = [0] * n
    for u, v in edges:
        graph[u].append(v)
        indegree[v] += 1

    order = []
    taken = [False] * n
    while len(order) < n:
        progress = False
        for u in range(n):
            if not taken[u] and indegree[u] == 0:
                order.append(u)
                taken[u] = True
                progress = True
                for v in graph[u]:
                    indegree[v] -= 1
        if not progress:
            raise ValueError("graph contains a cycle")
    return order
