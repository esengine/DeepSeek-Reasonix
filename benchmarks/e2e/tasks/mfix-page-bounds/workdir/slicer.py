from pager import page_bounds


def page_items(items, page, size):
    start, end = page_bounds(page, size)
    return items[start:end]
