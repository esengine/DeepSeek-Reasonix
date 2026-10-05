from pipeline.registry import register


def _ingest():
    return "veldst"


register("veldst", 10, _ingest)
