def format_receipt(items, total):
    lines = [f"{name}: {price:.2f}" for name, price in items]
    lines.append(f"TOTAL: {total:.2f}")
    return "\n".join(lines)

def format_legacy(items):  # dead code
    return str(items)
