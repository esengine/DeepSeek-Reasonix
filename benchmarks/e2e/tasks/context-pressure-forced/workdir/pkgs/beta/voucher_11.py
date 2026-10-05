import sys
sys.path.insert(0, "common")
from money import fmt_amount


def emit_voucher_0(rows):
    """Emit voucher rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def load_voucher_1(rows):
    """Load voucher rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(fmt_amount(value))
    return out

def render_voucher_2(rows):
    """Render voucher rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def collect_voucher_3(rows):
    """Collect voucher rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def summarise_voucher_4(rows):
    """Summarise voucher rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(fmt_amount(value))
    return out

def validate_voucher_5(rows):
    """Validate voucher rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def expand_voucher_6(rows):
    """Expand voucher rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def merge_voucher_7(rows):
    """Merge voucher rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(fmt_amount(value))
    return out

def filter_voucher_8(rows):
    """Filter voucher rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out
