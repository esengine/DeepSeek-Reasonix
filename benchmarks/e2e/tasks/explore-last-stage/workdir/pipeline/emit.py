from pipeline.registry import register


def _emit():
    return "quillshade"


register("quillshade", 4 * 5, _emit)
