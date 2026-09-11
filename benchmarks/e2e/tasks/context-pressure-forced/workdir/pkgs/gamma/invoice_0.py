import sys
sys.path.insert(0, "common")
from money import fmt_amount


def load_invoice_0(rows):
    """Load invoice rows for the gamma package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(fmt_amount(value))
    return out

def render_invoice_1(rows):
    """Render invoice rows for the gamma package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def collect_invoice_2(rows):
    """Collect invoice rows for the gamma package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def summarise_invoice_3(rows):
    """Summarise invoice rows for the gamma package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(fmt_amount(value))
    return out

def validate_invoice_4(rows):
    """Validate invoice rows for the gamma package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def expand_invoice_5(rows):
    """Expand invoice rows for the gamma package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def merge_invoice_6(rows):
    """Merge invoice rows for the gamma package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(fmt_amount(value))
    return out

def filter_invoice_7(rows):
    """Filter invoice rows for the gamma package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def rank_invoice_8(rows):
    """Rank invoice rows for the gamma package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out
