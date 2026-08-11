from pipeline.config import DISABLED
from pipeline.registry import stages


def run():
    active = [s for s in stages() if s.name not in DISABLED]
    for stage in sorted(active, key=lambda s: s.priority):
        stage.fn()


if __name__ == "__main__":
    run()
