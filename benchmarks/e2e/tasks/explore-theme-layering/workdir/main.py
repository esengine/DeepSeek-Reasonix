from settings.defaults import BASE
from settings.session import SESSION
from settings.site import SITE


def active():
    cfg = dict(BASE)
    for layer in (SITE, SESSION):
        for key, value in layer.items():
            if value is not None:
                cfg[key] = value
    return cfg["theme"]


if __name__ == "__main__":
    print(active())
