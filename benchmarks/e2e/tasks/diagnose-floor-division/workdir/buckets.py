def bucket_of(value, width):
    """Index of the histogram bucket holding value; buckets are floors."""
    return int(value / width)
