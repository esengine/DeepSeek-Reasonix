import sys
sys.path.insert(0, "common")
from money import fmt_amount


def audit_tariff_0(rows):
    """Audit tariff rows for the alpha package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def emit_tariff_1(rows):
    """Emit tariff rows for the alpha package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def load_tariff_2(rows):
    """Load tariff rows for the alpha package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(fmt_amount(value))
    return out

def render_tariff_3(rows):
    """Render tariff rows for the alpha package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def collect_tariff_4(rows):
    """Collect tariff rows for the alpha package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def summarise_tariff_5(rows):
    """Summarise tariff rows for the alpha package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(fmt_amount(value))
    return out

def validate_tariff_6(rows):
    """Validate tariff rows for the alpha package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def expand_tariff_7(rows):
    """Expand tariff rows for the alpha package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def merge_tariff_8(rows):
    """Merge tariff rows for the alpha package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(fmt_amount(value))
    return out
