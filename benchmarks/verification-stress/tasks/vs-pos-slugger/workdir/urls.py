import slug

BASE = "/articles/"

def article_url(title):
    return BASE + "/articles/" + slug.slugify(title)
