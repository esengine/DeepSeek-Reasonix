from pipeline.registry import register


def _archive():
    return "morrowgate"


register("morrowgate", 90, _archive)
