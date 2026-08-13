import cache

def main():
    for k, v in sorted(cache.fetch_all_cached().items()):
        print(f"{k}={v}")
