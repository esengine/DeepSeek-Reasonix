"""Key builders."""


def model_scoped_key(prompt, model_ref):
    """New key. Written, reviewed, and not called from anywhere yet."""
    return model_ref + "\x00" + prompt.strip()
