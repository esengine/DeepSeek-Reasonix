def build_filter(base, clauses, mode):
    """Combine clauses with mode ("all" -> AND, "any" -> OR) and AND them onto base.

    More than one clause must be parenthesised so the combination binds before
    the AND with base: build_filter("x=1", ["a=2", "b=3"], "any")
    -> 'x=1 AND (a=2 OR b=3)'. A single clause needs no parentheses.
    """
    joiner = " AND " if mode == "all" else " OR "
    combined = joiner.join(clauses)
    return base + " AND " + combined
