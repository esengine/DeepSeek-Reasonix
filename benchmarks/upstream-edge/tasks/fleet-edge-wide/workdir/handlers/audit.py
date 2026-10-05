def record(ctx, kind, total):
    ctx.setdefault("audit", []).append((kind, total))
