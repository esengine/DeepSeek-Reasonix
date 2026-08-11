def label_for(code):
    if code == "ok":
        return "Completed"
    elif code == "wip":
        return "In progress"
    elif code == "hold":
        return "On hold"
    else:
        return "Unknown"


def summary(codes):
    parts = []
    for code in codes:
        if code == "ok":
            parts.append("Completed")
        elif code == "wip":
            parts.append("In progress")
        elif code == "hold":
            parts.append("On hold")
        else:
            parts.append("Unknown")
    return ", ".join(parts)
