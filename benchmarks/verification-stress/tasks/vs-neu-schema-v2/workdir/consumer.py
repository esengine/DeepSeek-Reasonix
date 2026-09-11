import schema, serializer

def display_name(line):
    rec = serializer.load(line)
    return rec["username"] or rec["email"]
