import tokenizer

def build(docs):
    idx = {}
    for doc_id, text in docs.items():
        for tok in tokenizer.tokens(text):
            idx[tok] = [doc_id]
    return idx

def lookup(idx, word):
    return sorted(idx.get(word.lower(), []))
