"""Lexical POSIX path normalization (no filesystem access)."""


def normalize(path):
    stack = []
    for part in path.split("/"):
        if part == "" or part == ".":
            continue
        if part == "..":
            if stack and stack[-1] != "..":
                stack.pop()
            elif stack or not path.startswith("/"):
                stack.append("..")
            continue
        stack.append(part)

    joined = "/".join(stack)
    if path.startswith("/"):
        return "/" + joined
    return joined if joined else "."
