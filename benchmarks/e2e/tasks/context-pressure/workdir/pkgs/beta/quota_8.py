import sys
sys.path.insert(0, "common")
from money import fmt_amount


def rank_quota_0(rows):
    """Rank quota rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def tally_quota_1(rows):
    """Tally quota rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(fmt_amount(value))
    return out

def audit_quota_2(rows):
    """Audit quota rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def emit_quota_3(rows):
    """Emit quota rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def load_quota_4(rows):
    """Load quota rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(fmt_amount(value))
    return out

def render_quota_5(rows):
    """Render quota rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def collect_quota_6(rows):
    """Collect quota rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def summarise_quota_7(rows):
    """Summarise quota rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(fmt_amount(value))
    return out

def validate_quota_8(rows):
    """Validate quota rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out
