import sys
sys.path.insert(0, "common")
from money import fmt_amount


def tally_credit_0(rows):
    """Tally credit rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(fmt_amount(value))
    return out

def audit_credit_1(rows):
    """Audit credit rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def emit_credit_2(rows):
    """Emit credit rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def load_credit_3(rows):
    """Load credit rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(fmt_amount(value))
    return out

def render_credit_4(rows):
    """Render credit rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def collect_credit_5(rows):
    """Collect credit rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def summarise_credit_6(rows):
    """Summarise credit rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(fmt_amount(value))
    return out

def validate_credit_7(rows):
    """Validate credit rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def expand_credit_8(rows):
    """Expand credit rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out
