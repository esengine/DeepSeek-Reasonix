import paginate

def next_link(items, size, page):
    return f"?page={page + 1}" if page <= len(paginate.pages(items, size)) else None
