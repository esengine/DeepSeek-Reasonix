import slug

BASE = "/articles/"

def article_url(title):
    return BASE + slug.slugify(title)
