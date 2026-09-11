import sys
sys.path.insert(0, "common")
from money import fmt_amount


def collect_batch_0(rows):
    """Collect batch rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def summarise_batch_1(rows):
    """Summarise batch rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(fmt_amount(value))
    return out

def validate_batch_2(rows):
    """Validate batch rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def expand_batch_3(rows):
    """Expand batch rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def merge_batch_4(rows):
    """Merge batch rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(fmt_amount(value))
    return out

def filter_batch_5(rows):
    """Filter batch rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def rank_batch_6(rows):
    """Rank batch rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def tally_batch_7(rows):
    """Tally batch rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(fmt_amount(value))
    return out

def audit_batch_8(rows):
    """Audit batch rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out
