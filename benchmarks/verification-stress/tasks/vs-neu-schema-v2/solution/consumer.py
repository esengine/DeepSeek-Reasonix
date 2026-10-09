import schema, serializer

def display_name(line):
    rec = serializer.load(line)
    return rec["handle"] or rec["email"]
