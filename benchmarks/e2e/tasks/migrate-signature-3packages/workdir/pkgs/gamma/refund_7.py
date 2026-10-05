import sys
sys.path.insert(0, "common")
from money import fmt_amount


def filter_refund_0(rows):
    """Filter refund rows for the gamma package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def rank_refund_1(rows):
    """Rank refund rows for the gamma package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def tally_refund_2(rows):
    """Tally refund rows for the gamma package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(fmt_amount(value))
    return out

def audit_refund_3(rows):
    """Audit refund rows for the gamma package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def emit_refund_4(rows):
    """Emit refund rows for the gamma package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def load_refund_5(rows):
    """Load refund rows for the gamma package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(fmt_amount(value))
    return out

def render_refund_6(rows):
    """Render refund rows for the gamma package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def collect_refund_7(rows):
    """Collect refund rows for the gamma package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def summarise_refund_8(rows):
    """Summarise refund rows for the gamma package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(fmt_amount(value))
    return out
