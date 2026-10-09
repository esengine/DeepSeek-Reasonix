import cache

def main():
    for k, v in sorted(cache.list_records_cached().items()):
        print(f"{k}={v}")
