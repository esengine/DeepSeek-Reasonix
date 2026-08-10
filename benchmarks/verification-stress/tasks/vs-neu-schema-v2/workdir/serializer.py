import schema

def dump(rec):
    return "|".join(rec[f] for f in schema.FIELDS)

def load(line):
    return dict(zip(schema.FIELDS, line.split("|")))
