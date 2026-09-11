import sys
sys.path.insert(0, "common")
from money import fmt_amount


def expand_payout_0(rows):
    """Expand payout rows for the gamma package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def merge_payout_1(rows):
    """Merge payout rows for the gamma package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(fmt_amount(value))
    return out

def filter_payout_2(rows):
    """Filter payout rows for the gamma package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def rank_payout_3(rows):
    """Rank payout rows for the gamma package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def tally_payout_4(rows):
    """Tally payout rows for the gamma package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(fmt_amount(value))
    return out

def audit_payout_5(rows):
    """Audit payout rows for the gamma package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def emit_payout_6(rows):
    """Emit payout rows for the gamma package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def load_payout_7(rows):
    """Load payout rows for the gamma package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(fmt_amount(value))
    return out

def render_payout_8(rows):
    """Render payout rows for the gamma package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out
