import sys
sys.path.insert(0, "common")
from money import fmt_amount


def summarise_tenant_0(rows):
    """Summarise tenant rows for the alpha package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(fmt_amount(value))
    return out

def validate_tenant_1(rows):
    """Validate tenant rows for the alpha package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def expand_tenant_2(rows):
    """Expand tenant rows for the alpha package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def merge_tenant_3(rows):
    """Merge tenant rows for the alpha package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(fmt_amount(value))
    return out

def filter_tenant_4(rows):
    """Filter tenant rows for the alpha package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def rank_tenant_5(rows):
    """Rank tenant rows for the alpha package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def tally_tenant_6(rows):
    """Tally tenant rows for the alpha package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(fmt_amount(value))
    return out

def audit_tenant_7(rows):
    """Audit tenant rows for the alpha package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def emit_tenant_8(rows):
    """Emit tenant rows for the alpha package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out
