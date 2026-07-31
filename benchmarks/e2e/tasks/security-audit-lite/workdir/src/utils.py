import os

def handle_upload(filename, content, out_dir):
    # BUG 2: no validation — path traversal possible
    path = os.path.join(out_dir, filename)
    with open(path, "wb") as f:
        f.write(content)
    return path
